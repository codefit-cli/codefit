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
`reglas (código)` → `coverage.go` → `COVERAGE.md`.
- **Fuente-raíz:** `rules/<lang>/` + los sensores (`internal/sensors/`) + las reglas
  DB del núcleo (`internal/core/dbrules/` — las 8 reglas DB viven ahí; `internal/core/db/`
  es solo el modelo neutro `Schema`/`Table`/…, NO las reglas). Es lo que codefit realmente
  detecta.
- **Espejo-a-mano:** `internal/providers/<lang>/coverage.go` — NO es fuente pura; debe
  verificarse contra las reglas reales. El cierre lo chequea contra la raíz antes de
  espejar. Declararlo fuente pura reintroduce el drift un nivel más abajo — el drift
  silencioso que el pipeline existe para matar.
- **Excepción — dimensiones transversales (DB):** el modelo "un `coverage.go` por
  provider" NO cubre una dimensión que razona sobre el modelo neutro y no pertenece a un
  lenguaje. La dimensión DB NO tiene `coverage.go` propio: su prosa de cobertura vive
  EMBEBIDA (hoy en `internal/providers/typescript/coverage.go`) como DEUDA declarada,
  hasta que exista una fuente DB neutra (junto a `internal/core/dbrules/`). Al cerrar una
  dimensión transversal se verifica y edita esa prosa embebida contra las reglas reales,
  no un `coverage.go` per-lang inexistente.
- **Espejo 2º nivel:** `COVERAGE.md`, para humanos, mantenido a mano (no hay generador;
  verificado: sin `go:generate`, ningún `.go` emite markdown). Su encabezado promete
  auto-generación futura que no existe — claim stale, verificar la línea antes de tocar.

| Doc | Rol | Cadencia | Notas |
|-----|-----|----------|-------|
| `README.md` | fuente | resumen | Capacidades usables HOY, install, tools, puntero a roadmap. |
| `CHANGELOG.md` | fuente | resumen | Por release; lo realmente mergeado. Sin tags inventados. |
| `VERSIONING.md` | fuente | resumen | SemVer↔fase + estado actual. |
| `COVERAGE.md` | espejo (ver cadena) | resumen | Espejo 2º nivel, a mano. |
| `internal/providers/<lang>/coverage.go` | espejo-a-mano de las reglas | resumen | Se verifica contra la raíz; se edita ANTES que COVERAGE.md. |
| `CLAUDE.md` (este archivo) | fuente | resumen | Doctrina/método; el rollout apunta a PRD/VERSIONING/CHANGELOG. |
| `CONTRIBUTING.md` | fuente | por cambio | Proceso; no declara estado de fase. |
| `docs/decisions/NNNN-*` | fuente (append-only) | por slice | Un ADR por decisión de arquitectura; no se reescriben. |

**Exentos de la regla "reflejá lo de hoy" (diseño/visión, NO estado de entrega):**
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
- **El núcleo NUNCA importa un provider concreto.** El adapter MCP (o el plumbing
  que resuelva `language → provider`) es el único lugar que mapea lenguaje a
  provider. Los sensores solo conocen la interface `providers.LanguageProvider`.
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
