# CLAUDE.md — Reglas permanentes de codefit

Este archivo define las reglas que se siguen en **todas** las sesiones de
desarrollo de codefit. Tienen precedencia sobre cualquier comportamiento por
defecto. Ante conflicto entre estas reglas y el PRD, el PRD manda en scope y
diseño; estas reglas mandan en metodología y convenciones.

---

## Proyecto

codefit es una herramienta open source escrita en Go que **audita código
generado (parcial o totalmente) por IA**. Su premisa central es detectar todo
lo que el desarrollador nunca va a ver durante el desarrollo normal:
vulnerabilidades de seguridad, complejidad algorítmica que escala mal, problemas
estructurales de base de datos, riesgo de regresión en tests y problemas de
calidad que solo aparecen con revisión profunda. No reemplaza TDD, SDD, linters
ni scanners de infra: es la capa de auditoría independiente que valida que el
código generado sea seguro, correcto y escalable antes de mergear a producción.
Su principio rector es **"codefit audita lo que el desarrollador no va a ver
nunca"** — si una dimensión es visible durante el desarrollo normal, está fuera
de scope.

codefit es **MCP-first puro**: se opera **exclusivamente** como servidor MCP que
los agentes de IA (Claude Code, OpenCode, Codex, Cursor, VSCode, etc.) consumen
como un conjunto de herramientas. **codefit no tiene modo CLI de auditoría y no
gestiona ningún LLM propio.** Corre las capas determinísticas (patrones + AST),
mapea la **superficie** estructural de las clases que requieren razonamiento
(IDOR, authz, over-fetching), y devuelve `findings + surface` al agente, que
razona la superficie con **su propio LLM**. La inteligencia la pone el agente.
Esto materializa la **democratización**: cualquiera que ya codee con IA puede
auditar sin pagar API keys ni configurar infraestructura.

El binario sí expone unos pocos comandos de **plumbing** (que no auditan y no
usan LLM): `mcp serve`, `init`, `update`, `status`, `version`.

La arquitectura separa un núcleo universal de language providers
intercambiables, de modo que incorporar un lenguaje nuevo no toca el núcleo.

- **Módulo Go:** `github.com/codefit-cli/codefit` (org GitHub `github.com/codefit-cli`).
- **Licencia:** Apache 2.0
- **Binario / config:** binario `codefit`, config de proyecto `.codefit.yaml`.
  (No hay config global de LLM ni de auth — codefit no gestiona modelos.)
- **Fuente de verdad:** `docs/PRD-codefit-v1.4.md`. Ante **cualquier** duda de
  scope o diseño, consultarlo **antes** de decidir. El análisis cuantitativo
  (tokens, costos, tiempos) vive en `docs/codefit-analisis-tokens-costos.md`.

---

## Principio de autonomía del developer (INNEGOCIABLE)

**Siempre decide el developer. Nunca pasamos por encima de sus decisiones.
Siempre se informan las consecuencias.** Es transversal a todo el diseño:

- codefit **informa** `blocked` ante un crítico sin consentimiento, pero **no tiene
  poder sobre el git**: la conducta de bloquear el commit es **política del dev**.
  codefit propone; el dev dispone.
- El consentimiento de seguridad siempre **informa la consecuencia** y deja
  registro auditable (`accepted_by`, `accepted_at`, `reason`).
- codefit genera su **propia skill** (`codefit init`) y la coloca para los agentes
  detectados; **nunca toca el `AGENT.md`/`CLAUDE.md` del usuario**.
- Cuando codefit señala un bloqueo, explica *por qué* y *qué pasa si igual se
  avanza* — nunca un "no" sin fundamento.

---

## Metodología de desarrollo (OBLIGATORIA)

### TDD (Test-Driven Development) — estricto

- **NUNCA** escribir código de producción sin un test que falle primero.
- Ciclo innegociable: **red** (escribir test → verlo fallar) → **green**
  (implementar el mínimo para pasar) → **refactor**.
- Cada función pública necesita tests **antes** de implementarse.
- Cobertura objetivo: **> 80%** en `internal/core` y `internal/sensors`.
- Los tests son parte del entregable, no un extra opcional.
- codefit **se audita a sí mismo** (ver más abajo): el código sin tests será
  detectado por su propio sensor de tests. Predicamos con el ejemplo.

#### Un test verde no controla nada hasta que se lo hizo fallar a propósito

Correr, pasar y asertar **todavía no es controlar**. Un fixture que cortocircuita
antes de llegar a la rama que se está probando queda verde para siempre sin
proteger nada.

- **Probar cada test por MUTACIÓN**: romper el comportamiento exacto que el test
  dice proteger → verlo **fallar** → restaurar → verlo pasar. Dejar las dos
  corridas escritas donde las vea quien revise (mensaje del commit o del PR).
- **La forma que más se repite: el fixture armado a mano sostiene valores que el
  camino de producción NO puede producir.** Un `db.Table` al que el test le pone
  una `Pos` que el reducer real nunca setea, por ejemplo. El test lockea una
  realidad que no existe y el defecto queda invisible para la suite entera.
  Preferir siempre tests que manejen el parser/sensor REAL sobre structs armados
  a mano.
- Pregunta de bolsillo para cualquier test: **"¿qué tendría que romperse para
  que esto falle?"** Si la respuesta es "nada", es un adorno, no un candado.
- Aplica también al trabajo delegado: **"los tests pasan" describe una suite, no
  un control.** Exigir la mutación y su salida.

### SDD (Specification-Driven Development)

- Cada componente nuevo arranca con una **mini-spec antes de codear**: qué hace,
  qué recibe, qué devuelve, qué casos de borde maneja.
- La spec se escribe como **doc comment en la interface/struct** antes de
  implementar.
- Para features grandes: escribir la spec en `docs/specs/<componente>.md`
  primero.
- Flujo: **spec → tests (TDD) → implementación → verificación contra spec**.

### Consulta antes de codear

El autor del proyecto trabaja como **arquitecto de software** y usa la IA como
desarrolladora. **Antes de escribir código de alcance no trivial, presentar el
plan y esperar confirmación.** No avanzar a implementación de un componente
nuevo sin que el enfoque esté acordado.

---

## Disciplina de verificación (OBLIGATORIA)

La sección de documentación de más abajo cubre **no mentir en los docs**. Ésta
cubre algo distinto y previo: **saber si algo realmente corrió**. Cada regla acá
viene de un fallo real de este proyecto, no de un hipotético. **La confianza no
es evidencia.**

- **Una afirmación sobre ejecución exige ejecución.** Si una oración dice que
  algo corrió, pasó, compiló o dio verde, hay que correr exactamente eso, en
  exactamente ese estado, y leer la salida. Esto incluye los artefactos escritos:
  mensajes de commit, cuerpos de PR, comentarios y docs son **afirmaciones**.
  "Los tests pasan" es mentira si ese árbol exacto no se testeó. Para probar un
  commit intermedio: `git worktree add --detach` y correr ahí.
- **`-count=1` en el gate, siempre.** Un verde cacheado no es verde. Ya pasó:
  `go test -race ./...` devolvió exit 0 con ~90% de los paquetes cacheados.
- **Nunca mandar stderr a `/dev/null`** en un comando cuya salida sostiene una
  afirmación, y chequear el exit code. Un comando que falla (127) y uno que no
  encuentra nada son **indistinguibles** una vez descartado el error.
- **La ausencia necesita sonda positiva.** Antes de concluir "no hay ninguno",
  probar que la búsqueda funciona. Ya pasó dos veces, y una fue peor que un falso
  negativo: un grep de confirmación devolvió 3 hits que eran todos falsos
  positivos de un patrón demasiado ancho (`CLUSTER` matcheando `PRIMARY KEY
  CLUSTERED`), y habría confirmado una conclusión equivocada.
- **Un fixture se verifica por su CONTENIDO, no por su nombre ni por que exista.**
  El excerpt vendorizado de Pagila no es Pagila completo. Un test escrito contra
  un corpus que no tiene la forma que dice ejercitar **pasa por vacío**.
- **Los informes de subagentes, las lentes de review y las skills son
  afirmaciones, no evidencia.** Una lente puede decir CRITICAL y estar
  equivocada; otra puede declarar "garantizado a nivel de tipos" algo que es un
  `string` pelado. Se verifican, no se creen.
- **El blast radius se lee, no se adivina.** Antes de llamar bajo-impacto a un
  cambio, leer todos los llamadores y todos los caminos que llegan.
- **Preferir la fuente de verdad a una reimplementación.** Correr la función real
  sobre los datos reales, no una copia de la lógica en otro lado.

## Restricciones técnicas NO NEGOCIABLES

- **`CGO_ENABLED=0` SIEMPRE.** Ninguna dependencia puede requerir CGO. Verificar
  con `CGO_ENABLED=0 go build ./...` después de cada cambio.
- **Cross-compile** debe funcionar para `linux/amd64`, `linux/arm64`,
  `windows/amd64`.
- **Binario único**, sin dependencias de runtime.
- **Parsing de Go:** usar `go/ast` de la stdlib (no tree-sitter).
- **Otros lenguajes (fases futuras):** tree-sitter **puro Go, sin CGO**.
- **codefit nunca llama a un LLM.** Las dos capas que produce son determinístico
  (certeza 1.0) y superficie mapeada (a razonar por el agente). Nada de clientes
  de modelos, API keys ni wizards de auth en el código.

---

## Arquitectura (resumen del PRD, secciones 15–16)

- **Tres capas:** `core/` (universal) → `sensors/` (lógica de auditoría) →
  `providers/` (específico por lenguaje).
- El **núcleo NUNCA depende de un lenguaje específico**. Solo conoce la interface
  `LanguageProvider`.
- **Agregar un lenguaje = implementar `LanguageProvider`**, sin tocar el núcleo,
  los sensores, el MCP server ni el reporting. Si agregar un lenguaje obliga a
  tocar el núcleo, el diseño falló.
- **El MCP server es un adapter delgado**, no reimplementa lógica: traduce las
  llamadas a las tools (`codefit-scan-security`, `codefit-surface-*`,
  `codefit-scan-all`, `codefit-scan-endpoint`, `codefit-baseline-list` / `-accept` /
  `-prune`, `codefit-confirm-surface`, `codefit-coverage`) a invocaciones del núcleo.
  Stateless.
- **Pirámide de filtrado: capa 0 (cambios) → capa 1 (regex) → capa 2 (AST +
  reglas + mapeo de superficie).** codefit nunca pasa de la capa 2; la capa 3
  (razonamiento) la ejecuta el agente sobre el `surface` devuelto. Nunca enviar
  al agente para razonar lo que una capa más barata resuelve con certeza.
- **Mapeo de superficie completa, no quirúrgico:** el AST no decide *si* hay
  vulnerabilidad — enumera *toda* la superficie auditable de cada categoría, para
  que el agente razone sin puntos ciegos (PRD §10).
- **Motor de reglas:** matcher propio en Go que interpreta un **subset del formato
  Semgrep** (operadores core: `pattern`, `pattern-either`, `patterns`,
  `pattern-not`, `pattern-inside`, metavariables, `metavariable-regex`). **No** se
  embebe OpenGrep/OCaml. **No** se implementa `mode: taint` — su función la cubre
  el razonamiento del agente sobre la superficie (PRD §17).
- **CVEs:** vía **OSV.dev** (gratis, sin API key). codefit no mantiene base
  propia (PRD §18).

---

## Modelo de dimensiones de auditoría (doctrina — ver ADR 0016)

Cada dimensión (security, db, review, complexity, tests) sigue el mismo ciclo de
vida. **Fuente de verdad:**
`docs/decisions/0016-dimension-lifecycle-standalone-then-wired-to-scan-all.md`.
**ADRs fundacionales a leer al tocar la dimensión DB (o cualquier dimensión nueva):
0014 (modelo neutro), 0015 (reglas en el núcleo), 0016 (este ciclo de vida).** Los
ADRs no se cargan solos en sesión — leerlos explícitamente antes de empezar.

- Una dimensión = **sensor + reglas/parser/superficie + tool MCP standalone
  permanente** (`codefit-scan-<dim>`). La tool standalone no es andamiaje: se usa
  para auditar una sola dimensión on-demand.
- Se desarrolla **standalone** (slice por slice, TDD, dogfood) sin tocar `scan-all`
  hasta estar COMPLETA. El **cierre obligatorio (DoD) es cablearla a `scan-all`**:
  una dimensión no está lista hasta que `scan-all` la corre. Se diseña desde el
  slice 1 pensando en ese cableado.
- `scan-all` corre TODAS las dimensiones terminadas y cableadas. Que hoy corra solo
  security es el estado fiel (es lo único cerrado), no un bug.
- Dimensiones no-endpoint (DB): al cablear requieren **bucket propio** en `scan-all`
  (no el modelo endpoint-céntrico de ADR 0006) y encienden **`by_dimension`** como
  parte del cierre.
- **Lente permanente:** la lógica de reglas razona sobre el modelo neutro del núcleo
  y vive en el NÚCLEO (el provider solo parsea); si una regla necesita un dato
  específico del ORM/lenguaje, se enriquece el núcleo, no la regla; cada límite se
  lockea como test.

---

## Convenciones de código

- **Errores:** siempre envueltos con contexto — `fmt.Errorf("...: %w", err)`.
- **Logging:** `slog` estructurado.
- **Nombres:** descriptivos, sin abreviaturas crípticas.
- **Cada paquete** tiene un `doc.go` con su propósito.
- **Tools MCP:** nombres con prefijo `codefit-` y guión medio; familia
  `codefit-surface-*` para el mapeo de superficie.
- **Commits:** conventional commits (`feat:`, `fix:`, `test:`, `docs:`,
  `refactor:`). Sin atribución de IA ni `Co-Authored-By`.

---

## Documentación (reglas de honestidad — OBLIGATORIAS)

La documentación refleja lo que codefit **HACE HOY en `main`**, no lo que hará al
terminar una fase. Honestidad sobre el estado real, no promesas. Aplican cada vez
que se actualiza cualquier doc (README, CHANGELOG, COVERAGE.md, CONTRIBUTING, ADRs).

### Regla rectora: ¿se puede USAR hoy desde main?
Antes de escribir **cualquier** afirmación de capacidad, preguntarse: *¿esto se
puede usar hoy desde `main`?* Si la respuesta es "no, falta X" o "es del próximo
slice", NO escribirlo como capacidad presente — marcarlo como *en desarrollo* o no
incluirlo. Documentar lo que no existe es el mismo pecado que un manifiesto que
sobre-promete: erosiona la confianza. codefit documenta lo que hace, no lo que hará.

### Verificá, no asumas (contra el código, no contra la intención ni el PRD)
Antes de afirmar que un comando o capacidad funciona, **verificar la implementación
real** (¿es `notImplemented`/scaffolding, o corre de verdad?). Documentar desde lo
que el código hace, no desde el diseño. (Así se cazó que `mcp serve` era scaffolding
cuando el README decía "conectá codefit a tu agente".)

### Marcá el estado real
- Nota de estado visible cuando codefit —o una capacidad— no es usable end-to-end
  ("en desarrollo activo, Fase N").
- Separar **lo que funciona hoy** de **lo que está planeado**; el diseño/visión va
  enmarcado como diseño, no como capacidad ejecutable.

### Sin releases inventados
- CHANGELOG: pre-release sigue pre-release. **No inventar una versión/tag que no
  pasó** — verificar `git tag -l`. Sin compare-links a tags inexistentes.
- Conventional commits / Keep-a-Changelog; entradas por lo realmente mergeado.

### COVERAGE.md / manifiesto sincronizado
- El manifiesto en código (`providers/<lang>/coverage.go`) es la fuente de verdad;
  `COVERAGE.md` lo espeja para humanos.
- Debe reflejar lo que las reglas y categorías **realmente** detectan, **incluidos
  los límites conocidos** (md5 siempre-marca, reglas name-driven, frontera multi-hop
  al agente, detección por forma no por nombre). Un manifiesto que sobre-promete es
  un auditor en el que no se puede confiar.

### Posicionamiento honesto
- Documentar también **qué NO hace codefit y por qué** (complementa linters, no los
  reemplaza: lo visible en desarrollo normal es del linter; codefit audita lo
  invisible). Es honesto y posiciona.

### Mantené la doc en sync con el cambio
- Cuando el comportamiento cambia, actualizar en el **mismo cambio** la doc afectada:
  capacidades del README, CHANGELOG, COVERAGE.md, y la interface/ejemplos de
  CONTRIBUTING. Verificar que los links internos resuelven.

### Gate de docs
- Aun en cambios solo-doc, correr el gate (build + lint) por si se tocó algo embebido
  (`go:embed`). Self-audit verde.

---

## Mapa documental (fuente vs espejo — lo lee el skill de cierre documental)

Estos son los HECHOS del árbol de docs; la doctrina de cómo escribirlos está en la
sección anterior. Agregar/quitar un doc = editar esta tabla, nunca el skill. **Regla
de oro: se edita la FUENTE, después se espeja.**

**Cadena de verdad de cobertura (3 niveles):**
`reglas (código)` → `dbcoverage.go` (DB) + `coverage.go` (por lenguaje) → `COVERAGE.md`.
- **Fuente-raíz:** `rules/<lang>/` + los sensores (`internal/sensors/`) + las reglas
  de la dimensión DB, que **NO viven todas en un solo paquete** — son CUATRO raíces
  distintas (`internal/core/db/` no es ninguna de ellas: es solo el modelo neutro
  `Schema`/`Table`/…, NO las reglas):
  - `internal/core/dbrules/` — las 14 reglas schema-only (`dbrules.All()`), contadas
    contra el código, no contra este archivo: si agregás o quitás una, actualizá el
    número acá en el mismo cambio.
  - `internal/core/dwrules/` — la familia DW-0xx (star schema / SCD, el chequeo de
    índice columnar/analítico y el censo de particionado), que corre DENTRO del sensor
    DB con la clasificación de paradigma como segundo input. Son **7** reglas hoy
    (`dwrules.All()`: DW-001, DW-002, DW-005, DW-010, DW-011, DW-020, DW-021), contadas
    contra el código igual que `dbrules`: si agregás o quitás una, actualizá el número
    acá en el mismo cambio. La familia está COMPLETA: no queda ninguna DW-0xx sin
    construir. Tres de ellas — DW-005, DW-011 y DW-020 — son juicios de CENSO a nivel
    esquema: emiten **un ítem por esquema como máximo**, nunca uno por tabla, y por eso
    se abstienen como REGLA ENTERA cuando el censo no es confiable (un censo encogido
    que igual emite es peor mentira que el silencio).
  - `internal/core/paradigm/` — detección de paradigma y rol de tabla, más la
    supresión 3NF que aplica el sensor (`internal/sensors/db/`). Desde ADR 0037 el
    orden está **invertido**: el SCHEMA GATE juzga el esquema ANTES de que ninguna
    tabla reciba rol de warehouse; el paradigma ya no se deriva bottom-up de los roles.
  - `internal/core/crossrules/` — el cruce código×schema (DB-010/DB-013), que corre
    fuera del sensor, solo en `scan-all`, y cuyas categorías se unen al scope del
    baseline de db (ADR 0019/0029).
  Es lo que codefit realmente detecta.
- **Espejo-a-mano:** `internal/providers/<lang>/coverage.go` — NO es fuente pura; debe
  verificarse contra las reglas reales. El cierre lo chequea contra la raíz antes de
  espejar. Declararlo fuente pura reintroduce el drift un nivel más abajo — el drift
  silencioso que el pipeline existe para matar.
- **Dimensiones transversales (DB):** el modelo "un `coverage.go` por provider" NO
  cubre una dimensión que razona sobre el modelo neutro y no pertenece a un lenguaje.
  La dimensión DB tiene su PROPIA fuente neutra, `internal/core/dbcoverage/`
  (`dbcoverage.go`), junto a `internal/core/dbrules/` — **la deuda de ubicación que
  antes vivía EMBEBIDA en `internal/providers/typescript/coverage.go` está PAGADA**
  (relocada en 0.2.2). Cada `coverage.go` per-lang la compone por `append`, nunca la
  duplica a mano; `dbcoverage` no importa ningún provider (leaf puro). Al cerrar una
  dimensión transversal se verifica y edita `dbcoverage.go` contra **las CUATRO raíces
  de reglas** que espeja — `internal/core/dbrules/`, `internal/core/dwrules/`,
  `internal/core/paradigm/` y `internal/core/crossrules/` — ANTES de espejar a
  `COVERAGE.md`, la misma disciplina fuente-antes-que-espejo del resto de esta tabla.
  **Verificar contra una sola de las cuatro es exactamente cómo se cuela el drift:** ya
  pasó dos veces, y en la peor dirección posible — la FUENTE quedó menos veraz que su
  espejo. `dbcoverage.go` llegó a declarar que la familia DW no estaba construida
  (estaba, y el mismo archivo se contradecía en otras dos entradas) y a NEGAR el cruce
  código×schema (DB-010/DB-013 salieron en `v0.2.4`), mientras `COVERAGE.md` ya decía
  lo correcto. Es la fuente la que sirve `codefit-coverage` al agente: si miente, el
  agente le cree.
- **Espejo 2º nivel:** `COVERAGE.md`, para humanos, mantenido a mano (no hay generador;
  re-verificado en `docs/phase-2-documentation-sync`: sin `go:generate`, ningún `.go`
  emite markdown). Su encabezado YA NO promete auto-generación — esa promesa stale se
  removió; hoy el encabezado declara la cadena de 3 niveles y dice explícitamente que no
  hay generador. Lo que SÍ había que corregir ahí era otra cosa: llamaba "source of truth"
  a `coverage.go`, que es espejo-a-mano, no fuente pura — el drift un nivel más abajo que
  esta misma tabla advierte. Corregido en la misma pasada, junto al doc comment de
  `internal/providers/typescript/coverage.go`, que repetía el mismo claim.

| Doc | Rol | Cadencia | Notas |
|-----|-----|----------|-------|
| `README.md` | fuente | resumen | Capacidades usables HOY, install, tools, puntero a roadmap. |
| `CHANGELOG.md` | fuente | resumen | Por release; lo realmente mergeado. Sin tags inventados. |
| `VERSIONING.md` | fuente | resumen | SemVer↔fase + estado actual. |
| `COVERAGE.md` | espejo (ver cadena) | resumen | Espejo 2º nivel, a mano. |
| `internal/core/dbcoverage/dbcoverage.go` | fuente neutra de la dimensión DB | resumen | Se verifica contra las CUATRO raíces: `dbrules/`, `dwrules/`, `paradigm/`, `crossrules/`. Se edita ANTES que COVERAGE.md. |
| `internal/providers/<lang>/coverage.go` | espejo-a-mano de las reglas | resumen | Se verifica contra la raíz; se edita ANTES que COVERAGE.md. |
| `internal/mcp/server.go` (descripciones de las tools) | fuente cara-al-agente | resumen | Es lo ÚNICO que el agente lee antes de elegir una tool: si describe mal una capacidad, el agente le cree. Se verifica contra las reglas/handlers reales en cada cierre. Ya driftó una vez —`codefit-scan-db` decía "(Prisma today)" con el parser SQL-DDL shippeado desde `v0.2.0` y toda la familia DW-0xx sin mencionar. |
| `internal/scaffold/skill.go` (la skill que genera `codefit init`) | fuente cara-al-agente | resumen | Es lo PRIMERO que el agente lee, ANTES que las descripciones de las tools — y su `description` **gatea la carga**: si el trigger no nombra el dominio, la divulgación progresiva descarta la skill sin leerla y el agente no ve una skill incompleta, no ve ninguna. Ya driftó DOS FASES: la dimensión DB shippeó entre `v0.2.0` y `v0.2.5` mientras la skill seguía describiendo solo seguridad de endpoints, sin nombrar `codefit-scan-db`, `codefit-coverage` ni `codefit-check-cves`. Hoy hay candado: `TestSkillNamesEveryRegisteredTool` (en `internal/mcp`, el paquete de test que ya importa `scaffold`) falla si una tool registrada en `NewServer` no está nombrada en la skill ni declarada en `deliberatelyNotInSkill` con su razón; `TestSkillOmissionAllowlistHasNoGhosts` cuida la dirección inversa. El candado obliga a la DECISIÓN, no al contenido: al cerrar, igual se verifica que lo enseñado sea verdad. |
| `internal/providers/registry/registry.go` (la tabla `lenguaje → provider`, capacidad + exposición) | fuente cara-al-agente | resumen | Es la única tabla que las otras dos fuentes cara-al-agente de esta fila (`server.go`, `skill.go`) deben terminar reflejando: `Entry.Exposure` dice qué resolver admite cada lenguaje HOY, y `LanguageProvider.Capability()` dice qué implementa cada provider (reglas por familia, categorías de superficie, si hay manifiesto de cobertura) — el vocabulario de superficie que ambas usan vive en `internal/core/surface.ProviderCategories`, verificado contra el bloque de consts real (`vocabulary_test.go`, `go/ast`), nunca copiado a mano. Se edita ANTES que la skill o las descripciones de las tools cuando cambia qué lenguaje está registrado o expuesto (ADR 0064). |
| `docs/roadmap.md` | fuente (prioridades + estado) | por slice | Qué se ataca y en qué orden, con el criterio "alguien que baja codefit hoy no puede ser engañado". Ordena por **daño al usuario**, no por capacidad: primero lo que MIENTE o se rompe, después lo que impide conocer los límites, recién ahí lo nuevo. **Es un PUNTERO, no una cuarta copia** — no re-enumera los ~40 límites declarados de `COVERAGE.md`/manifiestos, apunta a ellos; duplicarlos crearía un cuarto lugar donde driftear. Se actualiza cuando una prioridad se toma o cuando aparece una deuda nueva. |
| `CLAUDE.md` (este archivo) | fuente | resumen | Doctrina/método; el rollout apunta a PRD/VERSIONING/CHANGELOG. |
| `CONTRIBUTING.md` | fuente | por cambio | Proceso; no declara estado de fase. |
| `docs/decisions/NNNN-*` | fuente (append-only) | por slice | Un ADR por decisión de arquitectura; no se reescriben. |
| `docs/specs/<componente>.md` | fuente (contrato de diseño) | por slice | La mini-spec SDD que se escribe ANTES de codear (§ Metodología): qué hace, qué recibe, qué devuelve, qué casos de borde. **No declara estado de entrega** — se enmarca como el PRD: su horizonte va a propósito por delante de `main`, y el cierre documental NO la reescribe para que coincida con lo shippeado. Lo que SÍ se verifica al cerrar es la dirección inversa: que la doc de estado (README/CHANGELOG/skill) documente lo que el código hace, no lo que la spec prometía. |

**Exentos de la regla "reflejá lo de hoy" (diseño/visión, NO estado de entrega):**
- `docs/specs/*.md` — contrato de diseño previo al código (ver la fila en la tabla).
- `docs/PRD-codefit-v1.4.md` — fuente de scope/diseño. Su "Estado actual" queda a
  propósito atrás de `main`. El cierre NO lo corrige; a lo sumo flipea markers de la
  tabla de tools.
- `docs/codefit-analisis-tokens-costos.md` — referencia arquitectónica.

**Fuera del set de estado/capacidad (el cierre no los toca):** `SECURITY.md`,
`CODE_OF_CONDUCT.md`, `rules/README.md` (doctrina del formato de reglas), plantillas
`.github/`, READMEs de `testdata/`.

### Registro al crear (obligatorio)
Al crear un doc nuevo que declare estado o capacidad (guía de usuario, referencia,
etc.), **registrarlo en esta tabla en el mismo acto**, con su rol y su cadencia. Un doc
que declara capacidad y no está acá queda fuera del cierre documental y se desincroniza
con el tiempo — el fallo exacto que este mapa previene.

---

## Comandos de desarrollo

```bash
make build          # compila con ldflags (version/commit/date) → bin/codefit
make test           # go test ./...
make lint           # golangci-lint run (config en .golangci.yml)
make cross-compile  # linux/amd64, linux/arm64, windows/amd64, darwin/arm64 → dist/
make clean          # borra bin/ y dist/

# Un solo test / un solo paquete
go test ./internal/core/scoring/ -run TestIsBlocked -v

# Cobertura de un paquete
go test -cover ./internal/sensors/security/

# Verificación obligatoria antes de un PR (lo que corre el CI)
CGO_ENABLED=0 go build ./...        # el no-CGO no es negociable
go vet ./... && go test -race ./...  # incluye self-audit + test de integración MCP
```

**Self-audit:** en el modelo MCP-first puro, el self-audit es un **test de
integración Go**, no un comando de terminal. Un test corre los sensores (vía su
API interna de Go) sobre el propio código de codefit y asegura que no haya
findings críticos. Un test de integración MCP aparte valida la capa de
transporte (levantar el server, llamar una tool, verificar la respuesta). Ambos
viven en `go test ./...` y corren en cada PR (PRD §26).

---

## Reglas de layering no obvias (respetarlas o se rompe el diseño)

- **`core/findings/` es una HOJA**: no importa ningún otro paquete de codefit.
  `AuditContext` vive en `core/context/` y no en `findings/` (tiene un
  `*config.Config`; ponerlo en `findings/` crearía un ciclo con el parser).
- **El núcleo NUNCA importa un provider concreto**, y el mapeo `lenguaje → provider`
  vive en **UNA sola tabla**, `internal/providers/registry`, única fuente de esa
  correspondencia. `internal/mcp` y `internal/scaffold` son **consumidores**: cada
  uno hace su propia CONSULTA sobre esa tabla — por nombre, por extensión, por
  marker file, por presencia de manifiesto — y **ninguno construye un provider
  concreto por su cuenta**. Ningún otro paquete de **producción** importa
  `internal/providers/<lang>`; un test externo sí puede construir un provider real
  como fixture (es preferible a un struct armado a mano, § Metodología), y eso no
  es mapeo. La tabla distingue **capacidad** — lo que el provider declara que
  implementa, sobre el vocabulario que ya vive en `internal/core/surface` — de
  **exposición** — qué resolvers lo admiten hoy: un lenguaje puede estar
  registrado y deliberadamente NO expuesto, y esa diferencia es exactamente lo
  que codefit le declara al agente. El vocabulario se queda en el núcleo porque
  **no nombra ningún provider**; la declaración de capacidad NO, porque nombra
  exactamente eso. **Queda fuera** el parser de esquema de la dimensión DB:
  resuelve por la forma del INPUT (`.prisma`/`.sql`), no por lenguaje (ADR 0018).
  Los sensores solo conocen la interface `providers.LanguageProvider`.
- **La interface `LanguageProvider` es agnóstica al parser** (ver
  `docs/decisions/`): el provider es dueño de su parser. **Go usa `go/ast`**;
  tree-sitter puro Go queda para TS/Java/Python.
- **Severidad contextual:** el sensor emite findings con su severidad "natural";
  el ajuste por `path_criticality` lo aplica el sensor con
  `cfg.PathCriticalityFor(path)`, no el provider. Findings de seguridad en
  archivos de test se degradan a `info` (configurable, PRD RF-10).
- **El bloqueo NO es configurable:** `scoring.IsBlocked` (critical de seguridad
  sin consent ni baseline) fuerza `Blocked: true`. codefit **informa**
  `blocked`; la conducta de bloqueo del commit es **política del dev** (codefit no
  tiene poder sobre el git del usuario; la skill no inyecta reglas de commit-block).

**Decisiones de arquitectura** se registran como ADR en `docs/decisions/`.

---

## Memoria del proyecto (Engram)

Usar Engram (MCP) para persistir entre sesiones:

- Decisiones arquitectónicas y su justificación.
- Estado de cada fase del rollout (qué está done, qué falta).
- Convenciones descubiertas durante el desarrollo.
- Falsos positivos conocidos del self-audit y cómo se resolvieron.

Protocolo de sesión:

- **Al inicio:** consultar Engram para recuperar el contexto del estado actual.
- **Al final:** persistir en Engram las decisiones tomadas y el progreso de la
  fase.

---

## Rollout y versionado

El rollout completo por fase, los criterios de "done" y la tabla de versionado
(SemVer, Fase→MINOR) son estado del proyecto y viven en su fuente, no acá:
`docs/PRD-codefit-v1.4.md` §25 (rollout + criterios), `VERSIONING.md` (SemVer↔fase
+ estado actual) y `CHANGELOG.md` (lo realmente mergeado por release). Este archivo
declara doctrina y método, no estado de fase.
