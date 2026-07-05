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

**Vulnerabilidades de dependencias:** `govulncheck` está **pinneado a `@v1.1.4`**
en `.github/workflows/security.yml`. Las versiones `>= ~v1.2` crashean
(`panic: ForEachElement ... *types.TypeParam`) analizando los generics de
`charmbracelet/huh`. No subir el pin hasta que el bug upstream esté resuelto.

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

## Rollout (PRD §25)

- **Fase 0** — Foundations + núcleo + Go provider (self-audit).
- **Fase 1** ✅ **COMPLETA (`v0.1.0`)** — TypeScript provider + sensor de seguridad
  completo (con mapeo de superficie) + **MCP server funcional** + `codefit init`
  (genera la skill de codefit y la coloca para los agentes detectados; **no toca el
  `AGENT.md`**) + **baseline** (memoria del proyecto: `scan-all` con delta +
  `baseline-list`/`-accept`/`-prune`). Dogfoodeado en un backend real.
- **Fase 2** ✅ **COMPLETA (`v0.2.0`)** — dimensión DB: modelo neutro + parsers Prisma
  y SQL-DDL (Flyway), 8 reglas schema-only OLTP (DB-050/001/011/002 estructurales +
  DB-051/052/053/003 heurísticas por nombre), `codefit-scan-db`, dimensión en `scan-all`
  (sección DB + baseline unificado) y `by_dimension`. Dogfoodeada en Prisma real y
  SQL-DDL/Postgres real. **Diferido:** N+1, índice-vs-query, reglas de vistas/procs/
  triggers, y OLAP.
- **Fase 3** — Code review + best practices + tests + riesgo de regresión.
- **Fase 4** — Knowledge packs + `codefit update` + manifiesto de cobertura
  (`COVERAGE.md` + tool `codefit-coverage`) + release pública `0.x`.
- **Post-1.0.0** — Java (`1.1`), Python (`1.2`). El sensor de complejidad empírica
  (sandbox Docker) se evalúa post-1.0 por requerir ejecución de código.

Versionado: SemVer, Fase→MINOR (Fase 1 = `0.1.0`, … Fase 4 = `0.4.0`, estable =
`1.0.0`); `v0.1.0` se reserva para Fase 1 completa. Ver el rollout completo, los
criterios de "done" y la tabla de versionado en el PRD §25 y `VERSIONING.md`.
