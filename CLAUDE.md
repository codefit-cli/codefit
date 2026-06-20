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

Opera en **dos modos sobre el mismo núcleo de sensores**: modo **CLI**
(reactivo, para terminal y CI/CD) y modo **MCP** (proactivo y stateless, donde
agentes de IA llaman los sensores como herramientas durante la generación de
código). La arquitectura separa un núcleo universal de language providers
intercambiables, de modo que incorporar un lenguaje nuevo no toca el núcleo.

- **Módulo Go:** `github.com/codefit-cli/codefit` (org GitHub `github.com/codefit-cli`).
  El PRD v1.2 fue alineado a `codefit-cli` en todas sus referencias de org/módulo/repo.
- **Licencia:** Apache 2.0
- **Binario / config:** binario `codefit`, config de proyecto `.codefit.yaml`,
  config global `~/.config/codefit/config.yaml`
- **Fuente de verdad:** `docs/PRD-codefit-v1.2.md`. Ante **cualquier** duda de
  scope o diseño, consultarlo **antes** de decidir. El análisis cuantitativo
  (tokens, costos, tiempos) vive en `docs/codefit-analisis-tokens-costos.md`.

---

## Metodología de desarrollo (OBLIGATORIA)

### TDD (Test-Driven Development) — estricto

- **NUNCA** escribir código de producción sin un test que falle primero.
- Ciclo innegociable: **red** (escribir test → verlo fallar) → **green**
  (implementar el mínimo para pasar) → **refactor**.
- Cada función pública necesita tests **antes** de implementarse.
- Cobertura objetivo: **> 80%** en `internal/core` y `internal/sensors`.
- Los tests son parte del entregable, no un extra opcional.
- codefit **se audita a sí mismo**: el código sin tests será detectado por su
  propio sensor de tests. Predicamos con el ejemplo.

### SDD (Specification-Driven Development)

- Cada componente nuevo arranca con una **mini-spec antes de codear**: qué hace,
  qué recibe, qué devuelve, qué casos de borde maneja.
- La spec se escribe como **doc comment en la interface/struct** antes de
  implementar.
- Para features grandes: escribir la spec en `docs/specs/<componente>.md`
  primero.
- Flujo: **spec → tests (TDD) → implementación → verificación contra spec**.

---

## Restricciones técnicas NO NEGOCIABLES

- **`CGO_ENABLED=0` SIEMPRE.** Ninguna dependencia puede requerir CGO. Verificar
  con `CGO_ENABLED=0 go build ./...` después de cada cambio.
- **Cross-compile** debe funcionar para `linux/amd64`, `linux/arm64`,
  `windows/amd64`.
- **Binario único**, sin dependencias de runtime.
- **Parsing de Go:** usar `go/ast` de la stdlib (no tree-sitter).
- **Otros lenguajes (fases futuras):** tree-sitter **puro Go, sin CGO**.

---

## Arquitectura (resumen del PRD, secciones 13–14)

- **Tres capas:** `core/` (universal) → `sensors/` (lógica de auditoría) →
  `providers/` (específico por lenguaje).
- El **núcleo NUNCA depende de un lenguaje específico**. Solo conoce la interface
  `LanguageProvider`.
- **Agregar un lenguaje = implementar `LanguageProvider`**, sin tocar el núcleo,
  los sensores, el MCP server, el CLI ni el reporting. Si agregar un lenguaje
  obliga a tocar el núcleo, el diseño falló.
- **Dos modos sobre el mismo núcleo:** CLI y MCP (stateless). El MCP server es un
  adapter delgado, no reimplementa lógica.
- **Pirámide de filtrado: regex → AST → LLM.** Nunca mandar al LLM lo que una
  capa más barata puede descartar o resolver. El orden de ejecución es una
  decisión de costo, no arbitraria.

---

## Convenciones de código

- **Errores:** siempre envueltos con contexto — `fmt.Errorf("...: %w", err)`.
- **Logging:** `slog` estructurado.
- **Nombres:** descriptivos, sin abreviaturas crípticas.
- **Cada paquete** tiene un `doc.go` con su propósito.
- **Commits:** conventional commits (`feat:`, `fix:`, `test:`, `docs:`,
  `refactor:`). Sin atribución de IA ni `Co-Authored-By`.

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
go vet ./... && go test -race ./...
./bin/codefit scan --no-llm --fail-on critical   # self-audit; debe quedar verde
```

**Vulnerabilidades de dependencias:** `govulncheck` está **pinneado a `@v1.1.4`**
en `.github/workflows/security.yml`. Las versiones `>= ~v1.2` crashean
(`panic: ForEachElement ... *types.TypeParam`) analizando los generics de
`charmbracelet/huh`. No subir el pin hasta que el bug upstream esté resuelto.

**Ejecutar el binario:** `scan` requiere un `.codefit.yaml` existente. `codefit
init` todavía es un stub, así que en un proyecto nuevo hay que escribir el config
a mano (ver el `.codefit.yaml` de la raíz como ejemplo).

---

## Arquitectura del código (as-built)

El árbol real difiere del esbozo del PRD §13 en algunos puntos; esto es lo que
está construido y por qué:

```
cmd/codefit/              # main: llama a internal/cli.Execute()
internal/
  cli/                    # cobra: 1 archivo por subcomando. scan/report/auth/set/status
                          #   funcionan; init/bench/review/run/baseline/mcp serve son stubs.
  config/                 # parser .codefit.yaml (validación ubicada path:línea) + config global
  auth/                   # keychain (go-keyring) + fallback AES-256-GCM; wizard (huh); resolver
  core/                   # NÚCLEO universal, language-agnostic:
    findings/             #   tipos base (Finding, Severity, Dimension, SensorResult) — HOJA
    context/              #   AuditContext (NO va en findings/, ver abajo)
    scoring/              #   ScoreSummary, Compute, IsBlocked
    report/               #   AuditReport canónico + renderers JSON/Plain/HTML + detección TTY
    pipeline/             #   pirámide (FilterLayer, Pipeline con early-exit) — construido, no wireado aún
    cache/                #   caché por hash SHA-256 — construido, no wireado aún
    llm/                  #   LLMClient + AnthropicClient (HTTP, prompt caching)
  sensors/                # sensores agnósticos al lenguaje:
    security/             #   pirámide regex (capa 1) + AST del provider (capa 2); capa LLM = skeleton
  providers/              # un provider por lenguaje:
    golang/               #   provider Go con go/ast (parse, security, practices)
  sandbox/                # gestor de contenedores Docker (para el sensor de complejidad)
  version/                # Version/Commit/BuildDate inyectados por ldflags
```

**Reglas de layering no obvias (respetarlas o se rompe el diseño):**

- **`core/findings/` es una HOJA**: no importa ningún otro paquete de codefit.
  Por eso `AuditContext` vive en `core/context/` y no en `findings/` — tiene un
  `*config.Config`, y si estuviera en `findings/` la capa de tipos base
  dependería del parser de config (ciclo).
- **El núcleo NUNCA importa un provider concreto.** El CLI (`scan.go
  resolveProvider`) es el único lugar que mapea `language → provider`. Los
  sensores solo conocen la interface `providers.LanguageProvider`.
- **La interface `LanguageProvider` es agnóstica al parser** (ver
  `docs/decisions/0001-...md`): expone `AnalyzeSecurity/AnalyzePractices(SourceFile)
  ([]Finding, error)`, NO queries de tree-sitter. El provider es dueño de su
  parser. **Go usa `go/ast` de la stdlib, no tree-sitter** (decisión cerrada para
  Go; tree-sitter queda para TS/Java/Python).
- **Severidad contextual**: el sensor emite findings con su severidad "natural";
  el ajuste por `path_criticality` (test baja un nivel, example → info) lo aplica
  el sensor con `cfg.PathCriticalityFor(path)`, no el provider.
- **El bloqueo de deploy NO es configurable**: `scoring.IsBlocked` (critical de
  seguridad sin consent ni baseline) fuerza `Blocked: true` y exit ≠ 0
  independientemente de `--fail-on`.

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

## Estado actual

- **Fase 0 (Foundations): completa.** Estructura de tres capas, interfaces
  `Sensor` y `LanguageProvider`, Go provider (go/ast), sensor de seguridad,
  `scan` end-to-end con self-audit, núcleo (scoring/report/llm/cache/pipeline),
  config + auth, CI/CD + goreleaser. Todo compila sin CGO y se auto-audita verde.
- **Diferido a fases siguientes** (construido pero no wireado, o stub): conectar
  `cache`/`pipeline` al scan; `--since` incremental (RF-08); aplicación de
  supresiones/baseline (RF-10); `init`/`bench`/`review`/`run`/`baseline`/`mcp
  serve`; capa LLM del sensor de seguridad.
- **Próximo (PRD §21):** Fase 1 — TypeScript provider + sensor de seguridad
  completo con capa 3 (LLM). Ver el rollout en el PRD, sección 21.
