# PRD — codefit
**Product Requirements Document v1.4**
**Estado:** Vigente · **Autor:** Lucas (Architect) · **Fecha:** Junio 2026

> **v1.4 — sync con la implementación (Fase 1).** Actualización quirúrgica que alinea el PRD con
> lo que codefit **es hoy en `main`**, sin reescribir la visión ni la arquitectura. Cambios:
> el onboarding pasó de enriquecer el `AGENT.md` a **generar una skill propia** que codefit coloca
> para los agentes detectados (§13); `scan-all` devuelve un **resumen de tres buckets** con detalle
> on-demand vía `scan-endpoint` (§11, §21); se documentan los **tres niveles de certeza** y el
> campo-hecho `StructuralFacts` (§10); el transporte MCP usa el **SDK oficial, solo stdio** (§7, §12);
> se sincroniza la **lista real de tools** y su estado implementada/stub (§11); y el **versionado
> SemVer** con el estado real de Fase 1 (§25, §30). Cada cambio está anclado en un ADR
> (`docs/decisions/0001`–`0008`). El v1.3 quedó en `docs/archive/PRD-codefit-v1.3.md`.
>
> **Enmienda (Fase 1 completa, `v0.1.0`):** se incorporó el **baseline** (RF-08) —
> `scan-all` con delta + `baseline-list`/`-accept`/`-prune`, identidad por contenido y
> salvaguarda graduada por certeza (§11, §25, §29; ADR 0009–0012). Fase 1 cerrada.

---

## Tabla de contenidos

1. [Resumen ejecutivo](#1-resumen-ejecutivo)
2. [Problema](#2-problema)
3. [Filosofía del producto](#3-filosofía-del-producto)
4. [Objetivos y no-objetivos](#4-objetivos-y-no-objetivos)
5. [Usuarios target](#5-usuarios-target)
6. [Visión del producto](#6-visión-del-producto)
7. [Modelo de operación: MCP-first](#7-modelo-de-operación-mcp-first)
8. [Los tres escenarios de uso](#8-los-tres-escenarios-de-uso)
9. [Requerimientos funcionales](#9-requerimientos-funcionales)
10. [Cobertura y garantías: mapeo de superficie](#10-cobertura-y-garantías-mapeo-de-superficie)
11. [Herramientas MCP — Diseño e interfaz](#11-herramientas-mcp--diseño-e-interfaz)
12. [Plumbing CLI — comandos mínimos](#12-plumbing-cli--comandos-mínimos)
13. [Onboarding: la skill de codefit](#13-onboarding-la-skill-de-codefit)
14. [Archivo de configuración de proyecto](#14-archivo-de-configuración-de-proyecto)
15. [Arquitectura técnica](#15-arquitectura-técnica)
16. [Arquitectura de extensibilidad: núcleo y language providers](#16-arquitectura-de-extensibilidad-núcleo-y-language-providers)
17. [Motor de reglas y formato](#17-motor-de-reglas-y-formato)
18. [Mantenimiento y actualización del conocimiento](#18-mantenimiento-y-actualización-del-conocimiento)
19. [Optimización de rendimiento y tokens](#19-optimización-de-rendimiento-y-tokens)
20. [Sensores — Especificación detallada](#20-sensores--especificación-detallada)
21. [Sistema de reporte](#21-sistema-de-reporte)
22. [Soporte de plataformas y lenguajes](#22-soporte-de-plataformas-y-lenguajes)
23. [Posicionamiento competitivo](#23-posicionamiento-competitivo)
24. [Complementariedad con SDD y TDD](#24-complementariedad-con-sdd-y-tdd)
25. [Rollout por fases](#25-rollout-por-fases)
26. [Self-audit y dogfooding](#26-self-audit-y-dogfooding)
27. [Roadmap futuro / Ideas](#27-roadmap-futuro--ideas)
28. [Métricas de éxito](#28-métricas-de-éxito)
29. [Decisiones resueltas](#29-decisiones-resueltas)
30. [GitHub y open source](#30-github-y-open-source)
31. [Glosario](#31-glosario)

---

## 1. Resumen ejecutivo

`codefit` es una herramienta open source, escrita en Go, que audita proyectos de software generados (parcial o totalmente) con IA. Su premisa central es detectar todo aquello que el desarrollador **nunca va a ver** durante el desarrollo normal: vulnerabilidades de seguridad, complejidad algorítmica que escala mal, problemas estructurales de base de datos, riesgo de regresión, y problemas de calidad de código que solo aparecen con revisión profunda.

codefit es **MCP-first**: se ejecuta como un servidor MCP (Model Context Protocol) que los agentes de IA (Claude Code, OpenCode, Codex, Cursor, VSCode, etc.) consumen como un conjunto de herramientas. El agente llama a codefit durante el desarrollo, recibe los hallazgos determinísticos y la superficie estructural a razonar, y completa el análisis con su propio LLM. codefit no gestiona ningún modelo de lenguaje propio: la inteligencia la pone el agente que ya está en uso. Esto materializa el objetivo de **democratizar la auditoría**: cualquiera que ya codee con IA —con una suscripción, un modelo local o un CLI de agente gratuito— puede auditar su código sin pagar API keys adicionales ni configurar infraestructura.

La arquitectura separa un **núcleo universal** (orquestación, optimización, reporting, transporte MCP) de **language providers** intercambiables, de modo que incorporar un lenguaje nuevo no requiere modificar el núcleo. codefit se distribuye como un binario único, sin CGO, para Linux y Windows.

No reemplaza TDD, SDD, linters ni herramientas de infraestructura. Es la capa de auditoría independiente que valida que el código generado sea seguro, escalable y de calidad antes de mergear a producción. **El developer siempre decide:** codefit informa, propone y advierte las consecuencias, pero nunca pasa por encima de las decisiones del desarrollador.

---

## 2. Problema

### Contexto

El desarrollo con IA (vibe coding, SDD, agentes autónomos, Claude Code, OpenCode) democratizó la escritura de código. Una descripción en un prompt produce una aplicación funcional. Esto es poderoso, pero desplaza responsabilidades que antes cubría la experiencia del desarrollador:

- El agente genera código que **pasa los tests** y **cumple los criterios de aceptación visibles**.
- Nadie verifica lo que no se ve: vectores de ataque, curvas de crecimiento, índices faltantes, procedimientos inseguros, tests que no cubren los caminos críticos, acoplamiento que rompe funcionalidades al agregar código nuevo.

El propio mercado lo reconoce: la generación de código con IA aceleró la velocidad de desarrollo, pero la capacidad de revisión humana no escaló al mismo ritmo, y el código generado por IA suele recibir menos escrutinio que el escrito a mano.

### Lo que el desarrollador no ve (y codefit sí)

| Dimensión | Por qué es invisible | Consecuencia si no se detecta |
|---|---|---|
| **Seguridad** | El código funciona; el ataque no ocurre en desarrollo | Vulnerabilidad en producción: breach, pérdida de datos, RCE |
| **Complejidad algorítmica** | Con datos de test (pocos registros), todo es rápido | Colapso de performance en producción con carga real |
| **Calidad de DB** | Las queries funcionan en desarrollo | Degradación exponencial a escala, inconsistencias de datos |
| **Code review profundo** | El desarrollador tiene blind spots sobre su propio código | Deuda técnica acumulada, imposibilidad de escalar el equipo |
| **Riesgo de regresión** | La nueva feature funciona; lo que rompió no tiene test | Bug silencioso en producción descubierto por un usuario |

### Por qué las herramientas existentes no alcanzan

- **TDD / Jest / JUnit / pytest**: verifican comportamiento funcional. No detectan vectores de seguridad, no miden complejidad empírica, no analizan la calidad estructural de la DB.
- **SDD (Specification-Driven Development)**: garantiza que el código implementa los requerimientos. No audita la calidad interna de la implementación.
- **Linters / SAST básicos (ESLint, Checkstyle)**: analizan sintaxis y patrones conocidos. No hacen inferencia semántica profunda.
- **AI PR reviewers (CodeRabbit, Greptile, Qodo)**: revisan el diff del PR en la nube, después de escribir el código. Su techo es el retrieval: cuando solo ven "el diff más 100 líneas alrededor", todos regresan al mismo límite.
- **Plataformas de verificación (SonarQube, Semgrep, Snyk)**: potentes y maduras, pero pensadas para CI/CD enterprise y equipos de más de 20 personas, con infraestructura y costo asociados.

codefit cierra la brecha entre "funciona" y "puede ir a producción", para el desarrollador que codea con IA y trabaja dentro de un agente.

---

## 3. Filosofía del producto

### Principio rector

> **codefit audita lo que el desarrollador no va a ver nunca.**

Si una dimensión es visible durante el desarrollo normal (la feature funciona, el test pasa, la UI renderiza), no es responsabilidad de codefit. Si es invisible (el vector de ataque, la curva O(n²), el índice faltante que no se nota con 100 registros), es exactamente donde codefit agrega valor.

### Principio de autonomía del developer

> **Siempre decide el developer. Nunca pasamos por encima de sus decisiones. Siempre se informan las consecuencias.**

Este es un principio transversal que gobierna cada decisión de diseño:
- codefit **informa** `blocked: true` ante un crítico de seguridad sin consentimiento, pero **no tiene poder sobre el git del usuario**: la conducta de bloquear (o no) el commit es **política del dev**. codefit propone; el dev dispone.
- El consentimiento de seguridad (un crítico puede aceptarse) siempre **informa la consecuencia** y deja registro auditable.
- codefit escribe **solo su propia skill** (ver sección 13), con la colocación informada al dev; **nunca toca el `AGENT.md`/`CLAUDE.md` del usuario**.
- Cuando codefit señala un bloqueo, siempre explica *por qué* y *qué pasa si igual se avanza* — nunca un "no" sin fundamento.

### Honestidad sobre la cobertura

codefit declara explícitamente qué clases de problemas detecta y cuáles **no** cubre (ver sección 10). Ninguna herramienta de auditoría detecta el 100% de los problemas —es matemáticamente imposible en el caso general—; lo que distingue a una herramienta seria es ser explícita sobre su alcance en vez de dar falsa confianza. Un reporte "sin hallazgos" siempre se califica con lo que ese reporte no miró.

### No invasividad

El código auditado no sabe que codefit existe. Sin decoradores, sin imports, sin modificaciones al código fuente del proyecto.

---

## 4. Objetivos y no-objetivos

### Objetivos

- Detectar vulnerabilidades de seguridad en el código antes del commit o del merge.
- Mapear de forma completa la superficie de cada clase de vulnerabilidad que requiere razonamiento, para que el agente la analice sin puntos ciegos.
- Auditar la calidad estructural de la base de datos: normalización, índices, vistas, procedimientos, OLTP y OLAP.
- Habilitar code review profundo combinando hallazgos determinísticos y superficie razonada por el agente.
- Auditar la calidad y cobertura de la suite de tests, e identificar riesgo de regresión.
- Funcionar como servidor MCP consumible por cualquier agente compatible.
- Democratizar la auditoría: cero costo de LLM propio, el agente pone la inteligencia.
- Soportar Linux y Windows con un binario único sin CGO.
- Ser completamente open source bajo Apache 2.0.

### No-objetivos (explícitos)

- **No tiene modo CLI de auditoría.** codefit no razona con un LLM propio ni audita desde la terminal de forma standalone. La auditoría ocurre exclusivamente vía MCP, donde el agente aporta el razonamiento. (El binario sí tiene comandos de *plumbing* mínimos: levantar el server, inicializar config, actualizar conocimiento, diagnóstico — ver sección 12.)
- **No verifica requerimientos funcionales.** Para eso existe SDD.
- **No ejecuta tests.** Para eso existe TDD. codefit audita la calidad y cobertura de la suite, pero nunca ejecuta `jest`, `pytest` ni `mvn test`.
- **No genera código** ni aplica fixes automáticos.
- **No monitorea producción** ni se integra con APMs.
- **No reemplaza un pentest.** Detecta patrones y mapea superficie; no hace explotación activa.
- **No gestiona credenciales de LLM.** En MCP, el LLM lo pone el agente.

---

## 5. Usuarios target

### Primario: desarrollador que codea con IA dentro de un agente

Escribe prompts, recibe implementaciones, las integra — todo dentro de un agente (TUI como OpenCode/Claude Code/Codex, o GUI como VSCode/Antigravity). Ya tiene un LLM configurado en su agente. Necesita que la auditoría ocurra en ese mismo contexto, sin salir a otra herramienta.

### Secundario: arquitecto que delega implementación a agentes

Diseña el sistema, delega la implementación a los agentes, y necesita una capa de auditoría que actúe como QA automatizado después de cada ciclo de generación. (Este es el perfil del propio autor del proyecto.)

### Terciario: usuario que hereda o quiere auditar un proyecto existente

Tiene un proyecto en curso o una release terminada y quiere una foto de su estado. Lo abre en su agente y dispara una auditoría completa con un comando conversacional.

### Perfil del early adopter

- Solo dev o equipo chico (1-5 personas).
- Ya codea con IA; tiene un agente con un LLM configurado (suscripción, local, o CLI gratuito).
- Stack inicial: Go (self-audit) y TypeScript/React + PostgreSQL.
- Corre Linux o Windows.
- Valora open source, transparencia y cero fricción de costos.

---

## 6. Visión del producto

### Propuesta de valor

> "codefit te dice si el código que generaste con IA puede ir a producción —no solo si funciona, sino si es seguro, escalable y no rompe lo que ya existe— sin salir de tu agente y sin pagar por otro LLM."

### Posicionamiento en el ecosistema

```
SDD          → garantiza que el código implementa los requerimientos
TDD          → garantiza que el código funciona según lo especificado
codefit      → garantiza que el código es seguro, escalable y de calidad

El agente genera código
     ↓
[SDD verifica que implementa los reqs]
     ↓
[TDD verifica que funciona]
     ↓
[codefit audita lo que no se ve]  ← vía MCP, el agente razona con su propio LLM
     ↓
Commit / merge / deploy
```

codefit no compite con ninguna de las otras capas. Las complementa. Las tres son necesarias; ninguna reemplaza a las otras (ver sección 24).

### Diferenciadores frente al mercado

- **MCP-first nativo**, no un add-on de una plataforma paga (ver sección 23).
- **Gate en el loop del agente**, local, antes del commit — no en el CI/CD enterprise. codefit informa `blocked`; el agente/dev decide la conducta (codefit no toca el git).
- **Mapeo de superficie completa**: ataca el techo de retrieval que limita a todos los AI reviewers (que solo ven el diff).
- **Cero costo de LLM**: la inteligencia la pone el agente del usuario.
- **Open source y honesto sobre su cobertura.**
---

## 7. Modelo de operación: MCP-first

codefit se ejecuta como un **servidor MCP**. Un agente de IA (el orquestador) lo consume como un conjunto de herramientas. No hay modo de auditoría por línea de comandos: toda la auditoría ocurre a través de llamadas MCP.

### Flujo general

```
Developer trabaja en su agente (Claude Code, OpenCode, Codex, VSCode...)
        ↓
El agente llama una herramienta MCP de codefit (ej: codefit-scan-security)
        ↓
codefit corre las capas determinísticas (patrones + AST) y mapea la superficie
        ↓
codefit devuelve: findings determinísticos + superficie a razonar (JSON)
        ↓
El agente razona la superficie con SU PROPIO LLM
        ↓
El agente reporta al developer / corrige / decide (orientado por la skill de codefit)
```

### codefit NO es un subagente

codefit es un **servidor de herramientas**, no un subagente del orquestador.

```
┌─────────────────────────────────────────────────────┐
│              ORQUESTADOR (el agente del dev)         │
│  - Decide el flujo                                  │
│  - Llama herramientas MCP de codefit                │
│  - Razona la superficie con su propio LLM           │
│  - Decide qué hacer con los findings (lo orienta    │
│    la skill de codefit; la conducta la define el dev)│
└──────────────────┬──────────────────────────────────┘
                   │ llama tools MCP (síncrono)
                   ▼
┌─────────────────────────────────────────────────────┐
│           CODEFIT (MCP server, proceso Go)          │
│  - Recibe la llamada                                │
│  - Corre capas determinísticas (patrones + AST)     │
│  - Mapea la superficie de las clases que requieren  │
│    razonamiento                                     │
│  - Consulta OSV.dev para CVEs                        │
│  - Devuelve findings + superficie en JSON           │
│  - NO razona, NO usa LLM propio, NO decide          │
└─────────────────────────────────────────────────────┘
```

Diferencia clave:
- Un **subagente** tiene su propio loop de razonamiento y toma decisiones.
- codefit como **MCP server** recibe una llamada, ejecuta lo determinístico, mapea superficie, devuelve. La inteligencia de razonar y decidir vive en el orquestador.

### codefit no gestiona LLM

En el modelo MCP-first, codefit **nunca llama a un LLM**. Las dos capas que produce son:

1. **Determinístico** (certeza 1.0): lo que codefit resuelve solo — secretos hardcodeados, índices faltantes, CVEs, antipatrones detectables por AST.
2. **Superficie mapeada**: para las clases que requieren razonamiento (IDOR, autorización, over-fetching), codefit enumera *toda* la superficie estructural relevante y se la entrega al agente, que razona con su propio LLM.

Esto elimina de raíz la necesidad de configurar API keys, wizards de autenticación, o backends de modelos en codefit. El LLM lo pone el agente que ya está en uso por el developer. Es el núcleo de la **democratización**: cualquiera que ya codee con IA puede auditar gratis.

### Modelo stateless

codefit es **stateless**: cada llamada MCP es independiente y no mantiene memoria de llamadas anteriores dentro de una sesión. El orquestador acumula los findings de todas las llamadas y decide.

Justificación:
- **Robustez:** no hay estado de sesión que pueda corromperse.
- **Simplicidad:** cada llamada es una función pura (entrada → salida).
- **Escalabilidad:** múltiples agentes pueden usar el mismo servidor sin interferencia.

### Transport

codefit usa el **SDK oficial de MCP** (`github.com/modelcontextprotocol/go-sdk`, auditado en
ADR 0007; `CGO_ENABLED=0` y cross-compile garantizados). Hoy se cablea **solo el transporte
stdio** (`codefit mcp serve`): el agente levanta codefit como subproceso local. El **HTTP/SSE
queda diferido** — el SDK abstrae el transporte, así que se agrega más adelante sin refactor.

---

## 8. Los tres escenarios de uso

codefit cubre tres situaciones distintas, todas vía MCP. La diferencia no es el mecanismo (siempre MCP), sino *cuándo* y *con qué granularidad* el agente llama a codefit.

### Escenario A — Desarrollo activo (el caso principal)

El developer está construyendo features con su agente. La **skill de codefit** (ver sección 13), que el agente carga por divulgación progresiva cuando la tarea matchea su `description`, lo orienta a invocar codefit en dos niveles:

```
Nivel 1 — Por unidad (corrección en caliente):
  El agente genera una función → llama codefit-scan-security sobre ella
  → si hay crítico, lo corrige ANTES de seguir con la próxima función.
  El bug nunca llega a existir como commit.

Nivel 2 — Antes del commit (el gate principal):
  La feature está completa → el agente llama codefit-scan-all sobre lo modificado
  → si hay crítico sin consentimiento, NO commitea: vuelve a implementar
    con el finding como contexto.
```

El bloqueo es **conductual y local**: vive en el loop del agente, en la compu del dev, antes del commit. No depende del CI/CD. Esto corrige los problemas antes de que existan como código versionado, que es mucho más temprano y valioso que un gate en el pipeline.

### Escenario B — Proyecto en curso

El developer conecta codefit a un proyecto que ya tiene código. Primer uso: dispara una auditoría completa con un comando conversacional en su agente (ej: `/codefit-scan`), que llama a `codefit-scan-all` sobre todo el proyecto. Esto establece la **línea base** (baseline): la deuda histórica se registra pero no genera ruido; de ahí en adelante, solo los cambios nuevos se auditan en modo Escenario A.

### Escenario C — Release terminada, usuario nuevo

Alguien que nunca usó codefit tiene una app terminada (incluso en producción) y quiere saber cómo está. Abre el proyecto en su agente, dispara `/codefit-scan`, y obtiene la foto completa del estado del release. No hay desarrollo en curso ni loop de generación: es una auditoría batch de una sola pasada, igual de simple que B pero sin baseline incremental posterior.

### Por qué los tres son MCP puro

El agente mismo es la interfaz de auditoría batch. El developer no necesita salir a una terminal: dispara un comando conversacional (`/codefit-scan`) y el agente llama a las tools MCP de codefit sobre el proyecto. Mismo resultado que tendría un CLI de auditoría, pero sin CLI — el "comando" lo da el agente, no la terminal. Esto vale para agentes TUI (OpenCode, Codex, Claude Code) y GUI (VSCode, Antigravity) por igual.

---

## 9. Requerimientos funcionales

### RF-01 · Sensor de Seguridad *(dimensión más crítica)*

Detecta vulnerabilidades de seguridad antes del commit o el merge. Combina dos capas: determinística (certeza 1.0) y superficie mapeada (razonada por el agente).

**Determinístico (capas 1-2, certeza 1.0):**
- Secretos y credenciales hardcodeadas (API keys, tokens, passwords, connection strings). IDs SEC-001 a SEC-009.
- SQL injection por concatenación / interpolación sin parametrizar. SEC-010 a SEC-013.
- Command injection (`exec`/`shell`/`subprocess` con input no sanitizado). SEC-014 a SEC-016.
- XSS directo (`dangerouslySetInnerHTML` con valor no constante). SEC-017 a SEC-019.
- JWT con algoritmo `none` o secreto débil hardcodeado. SEC-020.
- Criptografía débil (MD5/SHA1 para passwords, `Math.random()` para tokens, salts ausentes). SEC-050 a SEC-059.
- Configuración insegura (CORS `*` en producción, debug habilitado, headers de seguridad ausentes). SEC-040 a SEC-049.

**Superficie mapeada (razonada por el agente, ver sección 10):**
- IDOR: enumera todos los endpoints que reciben un ID y acceden a un recurso. SEC-021.
- Broken authorization: enumera todos los handlers protegibles. SEC-022.
- Over-fetching de datos sensibles: enumera todas las serializaciones de objetos de dominio. SEC-030 a SEC-039.

**CVEs (ver RF-09):** dependencias con vulnerabilidades conocidas vía OSV.dev. SEC-04x.

**Política de consentimiento:** los hallazgos `critical` de seguridad NO se suprimen con un `ignore.paths` normal. Requieren declaración explícita con `accepted_by`, `accepted_at` y `reason`. El reporte siempre los lista, aunque estén suprimidos, marcados como "aceptados con consentimiento".

### RF-02 · Sensor de Code Review

Code review profundo que combina los hallazgos determinísticos con la superficie mapeada, entregado al agente para que razone como un senior. Cubre lo que los linters no ven porque requiere comprensión del contexto:
- Legibilidad y claridad (nombres que no expresan intención).
- Complejidad cognitiva excesiva, anidamiento profundo, funciones con demasiadas responsabilidades.
- Duplicación de lógica que debería abstraerse.
- Manejo de errores ausente o que expone detalles internos.
- Inconsistencia con los patrones del proyecto.
- Código muerto, comentarios desactualizados.

A diferencia de los AI PR reviewers que analizan el diff aislado, codefit entrega al agente la superficie estructural completa con contexto del proyecto, atacando el techo de retrieval del mercado.

### RF-03 · Sensor de Base de Datos

Analiza la calidad estructural de las bases de datos. Soporta OLTP y OLAP.

**OLTP (PostgreSQL, MySQL, SQLite):**
- Normalización: FK sin índice (DB-001), columnas multivaluadas (DB-002), grupos repetidos (DB-003), violaciones candidatas de 2FN/3FN (DB-101/DB-102, vía razonamiento del agente).
- Índices: faltantes en columnas filtradas (DB-010), duplicados/redundantes (DB-011), nunca usados (DB-012), compuestos faltantes (DB-013).
- Vistas: exposición de columnas sensibles (DB-020), lógica que debería ser función (DB-021), materializadas sin refresh (DB-022), referencias rotas (DB-023).
- Procedimientos/funciones: SQL dinámico sin parametrizar (DB-030), sin manejo de excepciones (DB-031), side effects no documentados (DB-032).
- Triggers: cascadas no documentadas (DB-040), llamadas externas (DB-041).
- General: tabla sin PK (DB-050), TEXT como FK (DB-051), sin timestamps de auditoría (DB-052), campos sensibles sin cifrado (DB-053).

**OLAP / Data Warehouse / Data Mart:**
- Detección automática del paradigma (prefijos `fact_`/`dim_`/`stg_`/`mart_`, surrogate keys, desnormalización intencional).
- Esquema estrella/copo de nieve: hechos sin FK a dimensiones (DW-001), dimensión sin surrogate key (DW-002), ausencia de dimensión de tiempo (DW-005).
- SCDs: Type 2 sin índice en `is_current`/`valid_to` (DW-010), mezcla de estrategias (DW-011).
- Performance OLAP: hechos sin particionamiento (DW-020), sin índices columnares (DW-021), materializadas sin refresh (DW-022).
- En OLAP, la desnormalización intencional NO se reporta como violación de 3FN.

### RF-04 · Detección de patrones N+1

Detecta llamadas a la base de datos dentro de iteraciones (loops, `.map`, `.forEach`, `for`). DB-201.

### RF-05 · Sensor de Best Practices

Violaciones de las mejores prácticas del lenguaje/framework, especialmente frecuentes en código generado por IA. Las reglas concretas las aporta cada `LanguageProvider` (ver sección 16). Ejemplos para TypeScript/React: uso de `any`, props no tipadas, dependencias incorrectas en hooks, `useEffect` con lógica compleja, async sin manejo de errores, `console.log` en producción, variables de entorno sin validar.

### RF-06 · Sensor de Tests y Riesgo de Regresión

**No ejecuta tests.** Audita la calidad de la suite y calcula el riesgo de regresión.
- Ausencia total de tests (TEST-001), funciones críticas sin test (TEST-002), tests sin assertions (TEST-003), solo happy path (TEST-004), tests duplicados (TEST-005).
- Riesgo de regresión (modo incremental): qué funcionalidades existentes pueden verse afectadas por los cambios recientes — callsites afectados, funciones compartidas modificadas sin tests, cambios en schema que impactan queries. No dice si algo está roto; dice **qué puede estar roto**.

### RF-07 · Manifiesto de cobertura

codefit declara explícitamente qué clases de problemas detecta y cuáles no (ver sección 10). Se expone de dos formas, desde una única fuente de verdad por `LanguageProvider`:
- Como documento (`COVERAGE.md`) para el humano que evalúa adoptar codefit.
- Como herramienta MCP (`codefit-coverage`) para que el agente califique sus reportes en runtime e informe al developer qué quedó fuera del alcance.

### RF-08 · Baseline (adopción sin fricción)

`codefit-baseline` toma una foto del estado actual de findings. Con `baseline.enabled`, codefit solo reporta findings nuevos; la deuda histórica se registra (`baselined: true`) pero no genera ruido ni bloquea. Hace indolora la adopción en proyectos existentes (Escenario B).

### RF-09 · Análisis de dependencias con CVEs

Parsea los manifiestos de dependencias (`package.json`, `go.mod`, etc.), consulta **OSV.dev** (gratis, sin API key, agrega GitHub Advisory + bases de distros) y emite findings por cada CVE conocido en una versión usada. Severidad según el CVSS.

### RF-10 · Criticidad por contexto de path

codefit pondera la severidad de cada finding según la clasificación de path (production / test / example) declarada en config o aportada por el `LanguageProvider`. Un secreto en un test no pesa igual que en producción. Findings de seguridad en archivos de test se degradan a `info` (configurable).

### RF-11 · Inicialización y onboarding

`codefit init` analiza el proyecto por **marker files** y genera `.codefit.yaml` detectando lenguaje, framework, ORM, tipo/paradigma de DB y la ubicación del schema. Además **genera la skill propia de codefit** (un `SKILL.md` delgado, Anthropic Agent Skills Spec) y la **coloca donde cada agente detectado la descubre** (Claude Code, OpenCode, Codex). **No toca el `AGENT.md`/`CLAUDE.md` del usuario** y no escribe nada en silencio: informa cada archivo creado (ver sección 13).

---

## 10. Cobertura y garantías: mapeo de superficie

Esta sección define la garantía central de codefit contra los puntos ciegos, y es uno de sus diferenciadores clave.

### El problema del punto ciego

Ningún auditor —ni codefit, ni un humano, ni el mejor LLM— detecta el 100% de las vulnerabilidades. Es un límite teórico, no de implementación. La pregunta correcta no es "¿cómo no se nos escapa nada?" (imposible), sino "¿cómo maximizamos la cobertura y somos honestos sobre lo que no cubrimos?".

### Dos clases de detección

**Determinística:** codefit sabe exactamente qué busca (un `dangerouslySetInnerHTML`, un `md5()`, una concatenación SQL). El "punto ciego" aquí es solo "patrones que todavía no escribimos", que se cierra agregando reglas. Trabajo finito y acumulativo.

**Por razonamiento:** clases como IDOR, autorización rota u over-fetching no se detectan con un patrón fijo — requieren comprensión semántica. Aquí está el riesgo de punto ciego, y la solución es el mapeo de superficie.

### Mapeo de superficie completa (no quirúrgico)

El error de los enfoques ingenuos es marcar candidatos de forma *quirúrgica* (solo lo que el AST está casi seguro que es vulnerable), lo que hereda el punto ciego del AST. codefit hace lo contrario: marca candidatos de forma *amplia por categoría estructural*.

```
En vez de:
  "esta línea parece IDOR"  → restrictivo, punto ciego enorme

codefit hace:
  "TODOS los endpoints que reciben un ID y acceden a un recurso"  → superficie
  "TODAS las respuestas de API que serializan objetos de dominio"  → superficie
  "TODA construcción de query con input externo"                   → superficie
```

El AST no decide *si hay vulnerabilidad* — decide *qué superficie es auditable para cada clase*. El AST es excelente identificando estructuras ("esto es un endpoint", "esto serializa datos"), aunque sea malo juzgando intención. codefit entrega al agente **toda la superficie relevante**, no una muestra. El agente razona sobre cada elemento.

```
┌─────────────────────────────────────────────────────────┐
│ CAPA AST — mapeo de superficie (enumera, no juzga)      │
│   IDOR        → todos los endpoints con ID               │
│   Authz       → todos los handlers protegibles           │
│   Over-fetch  → todas las serializaciones                │
│   Injection   → toda construcción de query               │
│   Resultado: superficie COMPLETA por categoría           │
└────────────────────────┬─────────────────────────────────┘
                        │ toda la superficie relevante
                        ▼
┌─────────────────────────────────────────────────────────┐
│ EL AGENTE razona cada elemento con su propio LLM         │
│   y decide cuáles son realmente vulnerables              │
└─────────────────────────────────────────────────────────┘
```

Con esto, el punto ciego deja de ser "lo que el AST no sospechó" y pasa a ser solo "clases para las que no escribimos un mapeo de superficie" — finito, enumerable y documentado en el manifiesto de cobertura.

### Tres niveles de certeza (qué afirma codefit vs qué pregunta)

Cada concern que codefit emite lleva un **nivel de certeza** explícito, para que el agente sepa qué es un hecho y qué es una pregunta (ADR 0005/0006). Son tres:

```
deterministic     → codefit AFIRMA (regla, certeza 1.0): "esto es un md5()"
surface_confirmed → codefit PREGUNTA; vio la forma localmente: "este handler accede
                    a prisma sin un authz helper conocido — ¿es intencional?"
surface_frontier  → codefit PREGUNTA; el dato salió del cuerpo del handler, no pudo
                    concluir localmente: "no sigo llamadas entre funciones; seguí el dato"
```

El campo `affirms` distingue la afirmación (1.0) de la pregunta. codefit **nunca juzga** la superficie; afirma lo que es un hecho estructural y pregunta el resto.

### El campo-hecho (`StructuralFacts`)

Cada elemento de superficie carga **hechos estructurales** booleanos —no juicios— que el agente y la síntesis de `scan-all` usan para ordenar y clasificar sin razonar prosa:

```
local_access_detected   → el valor accede a un recurso vía un patrón local (ORM/DB)
known_authz_detected    → se detectó una llamada a un helper de autorización conocido
field_limiting_detected → la query limita columnas (select/omit)
```

Son la base del criterio resumen/on-demand de `scan-all` (sección 11): un concern resuelto localmente (`local_access_detected`) con su gap (p. ej. `known_authz_detected=false`) es **accionable**; sin gap es **resuelto-limpio**; lo que codefit no concluyó localmente es **frontier**.

### La frontera del mapeo: finito / infinito / no cubierto (ADR 0004/0005)

Cada categoría de superficie se parte en tres zonas, y el límite se declara, nunca se esconde:

- **FINITO** — superficie acotada que codefit **enumera completa**: canales de input HTTP (segmento de ruta, query, body, headers) y patrones de acceso local del ORM. Trabajo cerrado.
- **INFINITO** — seguir el dato a través de funciones/archivos es trabajo del **agente**; codefit enumera el **handoff** con una señal honesta y afirmativa del límite (no un "no detectado"). El wording es deliberado (guardado por test): *"codefit does not follow calls across functions, so this resource access is NOT verified here. Follow the data in the code to confirm"* — afirma el límite e invita a seguir, en vez de sugerir un falso negativo.
- **NO CUBIERTO** — lo que codefit no mapea se **declara** en el manifiesto, nunca queda silencioso.

Las **mejores prácticas por lenguaje** (qué cuenta como input, qué helper es de autorización, qué patrón es acceso a recurso) son la base del molde de cada categoría: el provider las aporta; el núcleo no las conoce.

### Reemplazo del taint analysis

Donde herramientas como Semgrep usan taint tracking estático (rastrear cómo un dato contaminado fluye hasta un sink peligroso), codefit usa razonamiento del agente sobre superficie completa. No se implementa taint estático (ver sección 17); su función la cubre el razonamiento sobre la superficie mapeada. Esto se declara en el manifiesto de cobertura.

### Manifiesto de cobertura

codefit declara, por lenguaje, qué cubre y cómo:

```
DETERMINÍSTICO (patrones codificados):
  ✓ Secretos hardcodeados, SQL/command injection, XSS directo,
    crypto débil, config insegura, índices, N+1, ...
RAZONAMIENTO (superficie mapeada → agente):
  ✓ IDOR, broken authz, over-fetching, violaciones 3FN candidatas
NO CUBIERTO (honestidad explícita):
  ✗ Race conditions en lógica de negocio
  ✗ Vulnerabilidades de diseño arquitectónico
  ✗ Lógica de negocio incorrecta (no es seguridad)
  ✗ Taint analysis estático profundo (se cubre por razonamiento, no determinístico)
```

El manifiesto se genera desde una única fuente de verdad en cada `LanguageProvider`, y se expone como `COVERAGE.md` (humano) y como tool `codefit-coverage` (agente en runtime). Esto convierte el punto ciego de "invisible y peligroso" a "declarado y conocido", y materializa el principio de informar siempre las consecuencias.
---

## 11. Herramientas MCP — Diseño e interfaz

Todas las herramientas usan el prefijo `codefit-` y palabras separadas por guión medio. Son stateless: reciben lo necesario como parámetros y devuelven JSON.

### Catálogo de herramientas

El estado **✅ / 🔲** es la verdad contra el código (`internal/mcp/server.go`): ✅ = registrada con
`addTool` y usable hoy; 🔲 = constante declarada pero **no expuesta** (stub planeada). Un colaborador
no debe confundir una stub con una tool usable.

| Tool MCP | Tipo | Estado | Qué hace |
|---|---|---|---|
| `codefit-scan-security` | Determinístico + superficie | ✅ Fase 1 | Seguridad sobre un proyecto. Input `{root, language}`; devuelve `findings + surface + score + blocked` |
| `codefit-scan-all` | Síntesis | ✅ Fase 1 | Resumen accionable por endpoint en **tres buckets** (ver abajo). Input `{root, language}` |
| `codefit-scan-endpoint` | Síntesis (on-demand) | ✅ Fase 1 | Re-analiza **un** archivo y devuelve el detalle completo de sus endpoints. Input `{root, language, file}` |
| `codefit-surface-idor` | Superficie | ✅ Fase 1 | Enumera la superficie IDOR (endpoints id→recurso) |
| `codefit-surface-authz` | Superficie | ✅ Fase 1 | Enumera la superficie de autorización (handlers sensibles) |
| `codefit-surface-overfetch` | Superficie | ✅ Fase 1 | Enumera la superficie de over-fetching (serializaciones) |
| `codefit-confirm-surface` | Cierre | ✅ Fase 1 | Integra los veredictos del agente: un item vulnerable pasa a finding probabilístico (confidence < 1.0) |
| `codefit-baseline-list` | Baseline | ✅ Fase 1 | Lista los items rastreados (fp, file, category, state; razón/fecha si acknowledged). `filter: known \| acknowledged`. Read-only |
| `codefit-baseline-accept` | Baseline | ✅ Fase 1 | Registra la decisión humana de aceptar item(s) (falso positivo / deuda asumida) con razón obligatoria |
| `codefit-baseline-prune` | Baseline | ✅ Fase 1 | Saca del baseline los items que un refactor resolvió (re-escanea para confirmar que están `gone`) |
| `codefit-baseline-record-verdict` | Cierre | ✅ Fase 3 | Persiste el veredicto del agente en `items[].agent_verdicts` (`by: agent`). Re-valida contra un re-análisis fresco: un veredicto cuyo item ya no está se RECHAZA y se nombra. Nunca acepta el item — solo un humano, con `-accept`. Dos agentes en desacuerdo: se guardan LOS DOS y el item queda `in_conflict` (ADR 0081) |
| `codefit-coverage` | Metadata | ✅ Fase 1 | Devuelve el manifiesto de cobertura para el lenguaje |
| `codefit-scan-db` | Determinístico + Superficie | ✅ Fase 2 (`v0.2.0`) | Estructura de DB schema-only (OLTP): sin PK, FK sin índice, índices duplicados, columnas array, heurísticas por nombre. Los diferidos ya NO lo están: vistas (`v0.2.2`), procs/triggers (`v0.2.3`), N+1 (`v0.2.2`, como `codefit-surface-nplus1`) y OLAP (familia DW-0xx completa, en `main` sin taggear) están todos cerrados |
| `codefit-check-cves` | Determinístico | ✅ Fase 1 (RF-09) | Consulta OSV.dev por las dependencias (versiones exactas de lockfile/go.mod) |
| `codefit-check-practices` | Determinístico | 🔲 Fase 3 | Best practices del lenguaje |
| `codefit-scan-tests` | Determinístico | 🔲 Fase 3 | Calidad de suite + riesgo de regresión |
| `codefit-review-code` | Combinada | 🔲 Fase 3 | Code review: determinístico + superficie, razonado por el agente |

Las `codefit-surface-*` son una familia conceptualmente distinta: no detectan, **enumeran superficie** para que el agente razone. Por eso llevan el segundo nivel `surface`.

### Contrato de respuesta — `scan-security` y `surface-*` (shape plano)

`codefit-scan-security` (y las `surface-*`, que devuelven solo `surface`) entregan el shape plano, subset del reporte canónico:

```json
{
  "findings": [
    {
      "id": "SEC-001",
      "dimension": "security",
      "severity": "critical",
      "file": "src/auth/jwt.ts",
      "line": 8,
      "title": "JWT secret hardcodeado",
      "description": "El secreto de firma JWT está embebido en el código.",
      "suggestion": "Mover a variable de entorno JWT_SECRET.",
      "confidence": 1.0,
      "probabilistic": false,
      "requires_consent": true
    }
  ],
  "surface": [
    {
      "category": "idor",
      "file": "src/routes/plants.ts",
      "line": 34,
      "snippet": "router.get('/plants/:id', (req,res) => repo.find(req.params.id))",
      "structural_facts": { "local_access_detected": true, "known_authz_detected": false },
      "reason_to_review": "Endpoint accede a recurso por ID; verificar ownership."
    }
  ],
  "summary": { "critical": 1, "high": 0, "medium": 0, "low": 0, "info": 0 },
  "blocked": true
}
```

`blocked: true` indica críticos de seguridad sin consentimiento; codefit lo **informa**, el agente/dev decide la conducta (codefit no toca el git).

### Contrato de respuesta — `scan-all` (resumen de tres buckets) + `scan-endpoint`

En un backend real el dump completo de superficie era tan grande (~101 items, ~80 KB) que los clientes MCP lo truncaban. Por eso `codefit-scan-all` devuelve un **resumen accionable de tres buckets** —decididos por hechos que codefit ya computa— y el detalle de cualquier endpoint nombrado queda a una llamada de `codefit-scan-endpoint` (ADR 0006/0008):

```json
{
  "summary": { "endpoints": 45, "certain_concerns": 21, "surface_items": 101 },
  "actionable":      [ /* EndpointReport completo: resuelto local CON gap. El agente actúa. */ ],
  "resolved_clean":  { "count": 11, "endpoints": [ /* nombrados + verification_fact: resuelto local SIN gap */ ] },
  "frontier_pending":{ "count": 24, "endpoints": [ /* nombrados: codefit no concluyó localmente */ ] }
}
```

- **`actionable`** — `CertainConcerns > 0` **y** `Actionable > 0`: resuelto localmente con un gap. Detalle completo.
- **`resolved_clean`** — `CertainConcerns > 0` **y** `Actionable == 0`: resuelto localmente, sin gap. **Nombrado + un hecho de verificación** ("el control esperado —authz / field selection— está presente localmente; codefit no verifica que sea *suficiente*"). NO es lo mismo que frontier (afirmación vs no-conclusión: se mantienen distintos a propósito).
- **`frontier_pending`** — `CertainConcerns == 0`: el dato salió del handler, codefit no concluyó localmente. Nombrado, no detallado. **No es un resultado limpio**: requiere que el agente lo siga.

`codefit-scan-endpoint {root, language, file}` re-analiza ese archivo (stateless, sin estado guardado) y devuelve el `EndpointReport` completo de un endpoint `frontier_pending`. Misma pipeline → el detalle es idéntico al que `scan-all` habría mostrado.

### El modelo de baseline (RF-08) — `scan-all` con delta + `list` / `accept` / `prune`

El baseline es un archivo commiteado (`.codefit-baseline`, raíz del repo — conocimiento compartido como `.codefit.yaml`) con la vista de codefit de la superficie auditada, para que un re-scan **solo muestre lo que cambió**. Propiedades clave:

- **Identidad por contenido, no por línea.** Cada item se fingerprintea por su contenido (categoría + file + snippet normalizado, **sin línea**), robusto a mover código; se re-detecta solo cuando el contenido cambia. El contenido se **hashea, nunca se guarda** → el texto de un secreto nunca llega al archivo commiteado (ADR 0009).
- **Foto del estado actual, no una lista de aceptados.** `scan-all` lee el baseline, reporta el delta — `new` / `changed` / `known` / `gone` —, persiste el baseline actualizado y filtra los buckets a lo no-rastreado. Lo `known` se silencia pero se cuenta (ADR 0010).
- **Salvaguarda graduada por certeza.** Una **pregunta** (superficie) pasa a `known` automático. Una **afirmación** (determinístico, certeza 1.0) **nunca se auto-silencia**: se muestra en cada scan hasta que un humano la acepta con razón. Silenciar una afirmación es más grave que silenciar una pregunta — el corazón de la decisión (ADR 0011).
- **Operado por el agente vía la skill, el humano decide.** `codefit-baseline-list` (ver pendientes), `-accept` (decisión humana, razón obligatoria, registra `by: human`), `-prune` (saca lo `gone`, re-escanea para confirmar). **codefit nunca toca el código ni el git** — solo su baseline (ADR 0012).

### Cómo el agente devuelve hallazgos de superficie (`codefit-confirm-surface`)

Cuando el agente razona la superficie y confirma una vulnerabilidad real, la integra al reporte con **`codefit-confirm-surface`** (tool implementada): el item vulnerable pasa a un finding **probabilístico** (confidence < 1.0) anclado al item. Stateless: codefit recomputa el id para validar. Así el razonamiento del agente queda registrado para baseline, score y trazabilidad — no solo en la conversación.

> **Nota de estado (2026-08-22).** La promesa de este párrafo — *baseline, score y trazabilidad* — la cumple **`codefit-baseline-record-verdict`**, no `confirm-surface`. `confirm-surface` es stateless por diseño (no recibe `root`, no puede escribir el baseline) y así se queda: la persistencia pertenece a la familia `baseline-*` (precedente del ADR 0013). El ciclo completo — persistir, reportar de vuelta en `scan-all`, y contar en el score — cerró en el ADR 0081 (H4, tres slices).

### Configuración en el cliente agente

Transporte **stdio**. Usar la **ruta absoluta** al binario si no está en el `PATH` del proceso del
agente; codefit es stateless, así que el root del proyecto va por arg (`root`) en cada tool, no por
`cwd`. Los bloques copy-paste por agente viven en el README ("Connect codefit").

**Claude Code** (`.mcp.json`):
```json
{
  "mcpServers": {
    "codefit": { "command": "/ruta/absoluta/codefit", "args": ["mcp", "serve"] }
  }
}
```

**OpenCode** (`opencode.json`):
```json
{
  "mcp": {
    "codefit": { "type": "local", "command": ["/ruta/absoluta/codefit", "mcp", "serve"], "enabled": true }
  }
}
```

**Codex** (`~/.codex/config.toml`):
```toml
[mcp_servers.codefit]
command = "/ruta/absoluta/codefit"
args = ["mcp", "serve"]
```

### Patrones de uso por el orquestador

**Patrón 1 — Auditoría tras generación (Nivel 1):**
```
genera código → codefit-scan-security(path) → evalúa → corrige o sigue
```

**Patrón 2 — Loop de auto-corrección (Nivel 2):**
```
1. codefit-scan-security → crítico
2. el agente NO llama otras tools (ahorra trabajo)
3. regenera con el finding como contexto
4. codefit-scan-security de nuevo → limpio
5. ahora sí: codefit-review-code, codefit-scan-db, etc.
6. avanza al commit
```

**Patrón 3 — Foto completa (Escenarios B y C):**
```
/codefit-scan → codefit-scan-all(path) → baseline (B) o reporte (C)
```

---

## 12. Plumbing CLI — comandos mínimos

codefit **no tiene modo CLI de auditoría**. El binario sí expone unos pocos comandos de *plumbing* —el mínimo irreducible para arrancar el server y administrar— que no auditan nada y no usan LLM. Es el mismo patrón de herramientas MCP-first como Engram.

| Comando | Qué hace |
|---|---|
| `codefit mcp serve` | Levanta el servidor MCP (stdio; SDK oficial, HTTP/SSE diferido). La razón de ser del binario. |
| `codefit init` | Genera `.codefit.yaml` y la **skill de codefit**, y la coloca para los agentes detectados. No toca el `AGENT.md` (ver sección 13). |
| `codefit update` | Descarga los últimos knowledge packs (ver sección 18). |
| `codefit status` | Diagnóstico: versión, `.codefit.yaml` encontrado, knowledge packs instalados, conectividad OSV.dev. |
| `codefit version` | Versión, commit y fecha de build. |

No hay `codefit scan`, `codefit review` ni `codefit bench` como comandos de terminal. Esa funcionalidad vive exclusivamente en las herramientas MCP.

---

## 13. Onboarding: la skill de codefit

El mecanismo por el cual codefit se integra al flujo del agente es su **propia skill**: `codefit init`
genera un `SKILL.md` delgado (siguiendo el **Anthropic Agent Skills Spec**) y lo coloca donde cada
agente detectado lo descubre. codefit **es dueño de su skill** —la genera, la versiona, y `update` la
regenera— y **nunca toca el `AGENT.md`/`CLAUDE.md` del usuario** (ese es territorio del dev).

> **Evolución desde v1.3.** El diseño anterior enriquecía el `AGENT.md` del usuario con reglas de
> orquestación. Se reemplazó por la skill propia: es más liviano, se apoya en la **divulgación
> progresiva** (el agente carga la skill solo cuando la tarea matchea su `description`), y respeta
> el `AGENT.md` del dev. El hallazgo de la industria pesó: **skills/configs inflados o generados por
> LLM son contraproducentes** —bajan el éxito del agente y suben el costo—; la skill de codefit es
> deliberadamente densa en señal y corta.

### Cómo funciona

`codefit init`:

1. **Genera la skill** — `SKILL.md` con frontmatter (`name` + `description`) y un cuerpo breve. La
   `description` lleva las palabras-trigger al frente (audit, security, IDOR, authz, over-fetching,
   "reviewing API endpoints / before merge"), clave para que la divulgación progresiva la dispare
   en el momento correcto. El cuerpo no repite lo que ya vive en codefit: **dispara y apunta a las
   tools** —cuándo correr `codefit-scan-all`, cómo seguir un `frontier_pending` con
   `codefit-scan-endpoint`, cómo leer los tres buckets— con comandos exactos (el `language` detectado
   horneado).
2. **La coloca por agente detectado** — detección por **marker (archivo o carpeta)** en la raíz del
   proyecto, con el mapa agente→ruta en un solo lugar (tabla de config):

   | Agente | Markers (archivo o carpeta) | Skill va a |
   |---|---|---|
   | Claude Code | `.claude` / `CLAUDE.md` | `.claude/skills/codefit/SKILL.md` |
   | OpenCode | `.opencode` / `opencode.json` | `.opencode/skills/codefit/SKILL.md` |
   | Codex | `.codex` | `.agents/skills/codefit/SKILL.md` |

   `AGENTS.md` **no** es marker (es convención cross-agent → daría falsos positivos). Para Codex la
   detección (`.codex`) y la escritura (`.agents/skills`) son asimétricas a propósito: `.codex` es su
   marcador propio, `.agents/skills` la ruta que Codex efectivamente lee.
3. **Si no detecta ningún agente conocido**, no falla: escribe la skill en la ubicación estándar
   `.agents/skills/codefit/` e informa cómo instalarla.
4. **Declara todo lo que hizo** — qué detectó y qué archivo creó. Nada en silencio.

### El bloqueo: codefit informa, el dev decide (corrección de honestidad)

codefit **informa** `blocked: true` ante un crítico de seguridad sin consentimiento, con el detalle
y la consecuencia. Pero **codefit no tiene poder sobre el git del usuario** y **la skill no inyecta
reglas de commit-block**: la skill orienta el loop de auditoría (correr las tools, leer los buckets,
seguir el frontier). La conducta de bloquear el commit es **política del dev** —puede instruir a su
agente en su propio `AGENT.md`, o decidir caso a caso—. codefit provee la información; el dev y su
agente definen qué hacer con ella.

### Autonomía del developer (principio innegociable)

- codefit escribe **solo su propia skill**, con la colocación **informada**; nunca toca el
  `AGENT.md`/`CLAUDE.md` del usuario.
- Si `.codefit.yaml` ya existe, `init` **no lo pisa en silencio**: avisa y pregunta (o `--force`).
- El developer decide la conducta de bloqueo; codefit informa las consecuencias de cada opción.

---

## 14. Archivo de configuración de proyecto

```yaml
# .codefit.yaml — se commitea al repositorio
version: "1"

project:
  name: "plantalinda-api"
  language: "typescript"          # go | typescript | java | python
  framework: "react"
  description: "Cannabis cultivation management platform"
  path_criticality:
    production: ["src/**", "*.config.ts"]
    test: ["**/*.test.ts", "**/*.spec.ts"]
    example: ["examples/**", "docs/**"]

database:
  paradigm: "oltp"                # oltp | olap | mixed | auto (default: auto)
  type: "postgresql"
  schema_paths: ["prisma/schema.prisma", "src/db/migrations/*.sql"]
  orm: "prisma"

sensors:
  security:
    enabled: true
    scan_dependencies: true       # CVEs vía OSV.dev
    test_severity: "info"         # info | downgrade | keep
  review:
    enabled: true
  db:
    enabled: true
    check_indexes: true
    check_views: true
    check_procedures: true
    check_triggers: true
  practices:
    enabled: true
    rules:
      no_any: "error"
      missing_error_handling: "warn"
      console_log_in_prod: "warn"
  tests:
    enabled: true
    test_dirs: ["src/**/*.test.ts", "src/**/*.spec.ts"]
    check_regression_risk: true

cache:
  enabled: true
  dir: ".codefit/cache"           # agregar a .gitignore

baseline:
  enabled: false
  file: ".codefit/baseline.json"  # foto del estado; se commitea

knowledge:
  auto_update: false              # si true, codefit update corre al arrancar el server

report:
  score_weights:                  # deben sumar 100
    security: 35
    review: 20
    db: 20
    complexity: 15
    tests: 10

ignore:
  paths: ["node_modules/**", "dist/**", "build/**", "**/*.generated.ts"]
  findings:
    - id: "DB-052"
      reason: "Sin timestamps por decisión de diseño en tablas de lookup"
    - id: "SEC-042"                # crítico de seguridad: requiere consentimiento
      reason: "CORS abierto intencional: API pública de lectura sin datos sensibles"
      accepted_by: "lucas@gentlemanprogramming.com"
      accepted_at: "2026-06-18"
```

Nota: no hay bloque `llm` ni `auth` — codefit no gestiona modelos en el modelo MCP-first. El razonamiento lo aporta el agente.

> **Estado (Fase 1).** Lo de arriba es el **schema de diseño** completo. Hoy `codefit init` emite el
> **subset mínimo y válido** (`version`, `project` con `path_criticality`, y `database` cuando detecta
> ORM/schema); el resto cae a defaults al cargar. Los bloques de sensores aún-no-implementados (`db`,
> `practices`, `tests`) y `knowledge`/`update` se materializan en sus fases (ver sección 25). El
> validador acepta solo valores dentro de los enums (p. ej. `framework: next`, no `nextjs`).

---

## 15. Arquitectura técnica

### Stack

| Componente | Tecnología | Justificación |
|---|---|---|
| Núcleo / MCP server / plumbing CLI | Go | Binario único sin runtime, cross-compile limpio, concurrencia nativa, baja barrera de contribución, goreleaser |
| Parsing Go | `go/ast` (stdlib) | Parser oficial del lenguaje, sin dependencias, sin CGO |
| Parsing TS/TSX | `gotreesitter` (tree-sitter **puro Go, sin CGO**, ADR 0002) | Preserva el binario único y el cross-compile; Java/Python quedan para fases post-1.0 |
| Motor de reglas | Matcher propio (subset formato Semgrep) | Reglas portables y conocidas por la comunidad, sin embeber OCaml (ver sección 17) |
| MCP server | SDK oficial `modelcontextprotocol/go-sdk` (ADR 0007), mismo binario | `codefit mcp serve` expone las tools por stdio sin proceso separado |
| CVEs | OSV.dev (HTTP) | Gratis, sin API key, agrega múltiples fuentes |
| Output | JSON → renderers | JSON canónico como fuente de verdad |

**¿Por qué Go y no Rust?** El cuello de botella de codefit no es CPU: el parsing es sub-milisegundo y el tiempo real está en I/O y, en el caso del agente, en el razonamiento del LLM (que codefit no ejecuta). Optimizar el parsing con Rust optimizaría ~2% del tiempo. Go ofrece compilación rápida, baja barrera de contribución (decisivo en open source) y el mejor tooling de distribución cross-platform.

**¿Por qué tree-sitter puro Go?** Los bindings tradicionales requieren CGO, lo que rompe el binario único y el cross-compile. Se usa una implementación pura en Go (sin CGO) para los lenguajes que la stdlib no parsea. Para Go se usa `go/ast`, que es superior por ser el parser oficial.

### Estructura del repositorio

```
codefit/
├── cmd/codefit/main.go
├── internal/
│   ├── cli/              # plumbing: serve, init, update, status, version
│   ├── mcp/              # MCP server: adapter tools MCP → núcleo
│   ├── config/           # parser .codefit.yaml
│   │
│   ├── scaffold/         # codefit init: detección + generación de config/skill + placement
│   ├── core/             # ── NÚCLEO UNIVERSAL (agnóstico al lenguaje) ──
│   │   ├── context/      # AuditContext
│   │   ├── syntax/       # Node: frontera AST agnóstica al parser (ADR 0003)
│   │   ├── pipeline/     # pirámide de filtrado (capas 0-2)
│   │   ├── surface/      # framework de mapeo de superficie
│   │   ├── cache/        # caché de findings por hash
│   │   ├── scoring/      # scores por dimensión y global; IsBlocked
│   │   ├── baseline/     # foto y diff de findings (en desarrollo)
│   │   ├── report/       # síntesis por endpoint (3 buckets) + JSON canónico
│   │   ├── findings/     # Finding, SurfaceItem (con StructuralFacts), Severity, ConsentRecord
│   │   ├── ruleengine/   # matcher formato-Semgrep (subset core)
│   │   ├── cve/          # cliente OSV.dev
│   │   └── coverage/     # manifiesto de cobertura
│   │
│   ├── sensors/          # ── SENSORES (orquestan capas, piden al provider) ──
│   │   ├── sensor.go     # interface Sensor
│   │   ├── security/  review/  db/  practices/  tests/
│   │
│   └── providers/        # ── LANGUAGE PROVIDERS (por lenguaje) ──
│       ├── provider.go   # interface LanguageProvider
│       ├── golang/       # provider de arranque (self-audit)
│       ├── typescript/   # primer target de producto
│       ├── java/         # (v1.1)
│       └── python/       # (v1.2)
├── rules/                # reglas declarativas formato-Semgrep, por lenguaje
├── docs/
└── README.md
```

### Interfaz Sensor y tipos

```go
type Sensor interface {
    Name()      string
    Dimension() Dimension
    Run(ctx AuditContext) (SensorResult, error)
}

type Finding struct {
    ID, Title, Description, Suggestion string
    Dimension     Dimension
    Severity      Severity
    File          string
    Line          int
    Reasoning     string         // por qué se marcó (findings razonados por el agente)
    Confidence    float64        // 1.0 determinístico
    Probabilistic bool           // true si vino del razonamiento del agente
    RequiresConsent bool
    Baselined     bool
    Suppressed    *ConsentRecord
}

type SurfaceItem struct {
    Category        string          // "idor" | "authz" | "overfetch" | ...
    File            string
    Line            int
    Snippet         string
    StructuralFacts map[string]bool // hechos (no juicios): local_access_detected, known_authz_detected, field_limiting_detected
    ReasonToReview  string          // la pregunta a razonar (no una afirmación de vuln)
}

type SensorResult struct {
    Sensor     string
    Score      int
    Findings   []Finding
    Surface    []SurfaceItem   // superficie a razonar por el agente
    DurationMs int64
    Error      string
}
```

### Flujo de una llamada MCP (ej: codefit-scan-security)

```
agente llama codefit-scan-security(path, since?)
│
├── el adapter MCP traduce a una invocación del núcleo
├── resuelve el LanguageProvider según project.language
├── Capa 0: filtro de cambios (since / hash de caché)
├── Capa 1: patrones (regex de alta precisión) → findings determinísticos
├── Capa 2: AST + reglas formato-Semgrep → findings determinísticos
│            + mapeo de superficie (IDOR, authz, overfetch) → SurfaceItem[]
├── aplica path_criticality y supresiones (con validación de consentimiento)
├── calcula blocked (críticos de seguridad sin consentimiento)
└── devuelve { findings, surface, summary, blocked }
```

codefit nunca pasa de la Capa 2. La Capa 3 (razonamiento) la ejecuta el agente sobre el `surface` devuelto.

---

## 16. Arquitectura de extensibilidad: núcleo y language providers

La decisión que garantiza escalar a cada lenguaje nuevo **sin tocar el núcleo**.

```
┌─────────────────────────────────────────────────────┐
│   NÚCLEO UNIVERSAL (core/) — agnóstico al lenguaje   │
│   orquestación · pirámide · superficie · caché ·     │
│   scoring · baseline · reporting · ruleengine ·      │
│   cve · coverage · MCP server                        │
└──────────────────────┬───────────────────────────────┘
                       │ depende de la interface
                       ▼
┌─────────────────────────────────────────────────────┐
│   LANGUAGE PROVIDER (interface)                      │
│   - parser (go/ast o tree-sitter puro Go)            │
│   - reglas determinísticas (formato Semgrep)         │
│   - queries de mapeo de superficie                   │
│   - mapeo de ORM/schema al modelo común de DB        │
│   - defaults de path_criticality                     │
│   - definición de cobertura (fuente del manifiesto)  │
│   - detección de tests del ecosistema                │
└─────────────────────────────────────────────────────┘
```

### La interface LanguageProvider

Esta es la **forma de destino** del provider (la superficie completa que tendrá al cerrar las fases):

```go
type LanguageProvider interface {
    Language()        string
    Frameworks()      []string
    FileExtensions()  []string

    Parse(path string) (*AST, error)           // go/ast o tree-sitter puro Go

    SecurityRules()   []Rule                    // reglas determinísticas (formato Semgrep)
    PracticeRules()   []Rule
    NPlusOneRules()   []Rule

    SurfaceQueries()  []SurfaceQuery             // mapeo de superficie por categoría

    ParseSchema(paths []string) (*DBSchema, error)

    DefaultPathCriticality() PathCriticality
    CoverageManifest()       CoverageManifest    // fuente única: doc + tool
    DetectTests(ctx AuditContext) ([]TestFile, error)
}
```

> **Estado real (ADR 0001/0003).** La interface implementada hoy es **parser-agnóstica**: el provider
> es dueño de su parser y expone **análisis que devuelve resultados** —`AnalyzeSecurity`,
> `AnalyzePractices`, `AnalyzeSurface` (`SourceFile → findings/surface`)— en vez de exponer el parser.
> El núcleo navega el AST solo a través de `core/syntax.Node`. La convergencia a la forma de destino
> de arriba (agregar `Parse` + reglas declarativas a la interface compartida) se **difirió** hasta que
> Go y TypeScript ejerciten ambos el modelo (ADR 0003). El contrato clave —**agregar un lenguaje no
> toca el núcleo**— se mantiene en las dos formas.

### Qué se escribe al agregar un lenguaje

Para incorporar, por ejemplo, Rust o Ruby: el parser (tree-sitter puro Go ya soporta 200+ gramáticas), las reglas determinísticas en formato Semgrep, las queries de mapeo de superficie, el parser de su ORM/schema, los defaults de criticidad, el manifiesto de cobertura y la detección de tests. **Cero cambios en el núcleo, los sensores, el ruleengine, el MCP server o el reporting.** Esto permite que la comunidad aporte lenguajes sin entender el motor.

### Sensores agnósticos

Los sensores viven en el núcleo y no saben qué lenguaje auditan. Orquestan las capas y piden datos al provider activo. El sensor de seguridad pide `provider.SecurityRules()` y `provider.SurfaceQueries()`, los corre contra el AST del provider, y emite findings + superficie. No sabe si es TypeScript o Go.

---

## 17. Motor de reglas y formato

### Decisión: formato Semgrep + matcher propio (sin embeber OCaml)

Las reglas determinísticas se escriben en un **subset del formato de reglas de Semgrep** (YAML declarativo, estándar de facto con miles de reglas de comunidad y un ecosistema conocido por los profesionales de seguridad). codefit implementa un **matcher propio en Go** sobre los AST de tree-sitter / go/ast, que interpreta ese subset. No se embebe el motor de Semgrep/OpenGrep (escrito en OCaml), porque rompería el binario único sin CGO.

### Operadores soportados (subset core)

Validado contra las reglas reales de TypeScript/React de Semgrep: el grueso de las reglas determinísticas de alto valor usa solo operadores core, perfectamente implementables sobre tree-sitter.

```
Soportado (MVP del matcher):
  ✓ pattern              (match simple)
  ✓ pattern-either       (OR)
  ✓ patterns             (AND)
  ✓ pattern-not          (exclusión)
  ✓ pattern-inside       (contexto)
  ✓ metavariables ($VAR)
  ✓ metavariable-regex

NO soportado (y está bien):
  ✗ mode: taint          → reemplazado por razonamiento del agente sobre superficie
  ✗ pattern-sources / pattern-sinks / pattern-sanitizers
```

### Por qué esto funciona

tree-sitter usa el mismo formato de queries para todos los lenguajes: una regla conceptual ("llamada dentro de un loop") tiene la misma estructura en TS, Java y Python, cambiando solo los nombres de nodos. Muchas reglas se escriben una vez y se parametrizan por lenguaje.

El taint analysis —lo único que justificaría embeber OpenGrep— se cubre por el camino del razonamiento: donde Semgrep necesita rastrear flujo de datos hasta un sink, codefit mapea la superficie (sources y sinks como elementos a razonar) y el agente razona el flujo. Esto se declara explícitamente en el manifiesto de cobertura.

### Dos formatos, dos propósitos

```
Reglas determinísticas (capas 1-2)  → formato Semgrep (subset core), estándar, comunidad
Mapeo de superficie (para el agente) → formato propio de codefit (SurfaceQuery)
```

Se usa el estándar donde el estándar es bueno, y formato propio solo donde codefit aporta algo que no existe.

---

## 18. Mantenimiento y actualización del conocimiento

Para que codefit no nazca actualizado y muera obsoleto, el conocimiento se actualiza en tres capas, cada una a su propia velocidad y por quien corresponde.

| Qué | Velocidad de cambio | Dónde vive | Cómo se actualiza |
|---|---|---|---|
| Reglas de detección (IDOR, injection, crypto) | Lenta (años) | `rules/*.yaml` en el binario | Release de codefit (PRs de comunidad) |
| Best practices por versión de framework | Media (meses) | Knowledge packs (repo aparte) | `codefit update`, sin reinstalar |
| CVEs (vulns concretas) | Rápida (días/horas) | OSV.dev (no se almacena) | En cada scan, siempre fresco |

### Capa 1 — Reglas de detección (en el binario)

Los principios de seguridad cambian muy lento. Se definen como **reglas declarativas** (formato Semgrep, sección 17), no hardcodeadas en Go, para que la comunidad contribuya sin saber Go. Se versionan con el binario.

### Capa 2 — Knowledge packs (actualizables aparte)

El conocimiento que depende de la versión del framework del usuario (ej: "en React 19 este patrón está deprecado") vive en **knowledge packs versionados**, en un repositorio separado (`codefit-cli/knowledge`), distribuidos como releases. codefit detecta la versión del framework (lee `package.json`) y carga el pack correspondiente.

```
codefit update                 # baja los últimos knowledge packs
~/.codefit/knowledge/
  react/19.x.pack.yaml
  nextjs/15.x.pack.yaml
  prisma/6.x.pack.yaml
```

Cuando sale React 20, se publica `20.x.pack.yaml` y el usuario hace `codefit update` — sin actualizar el binario. Desacopla el conocimiento del motor.

### Capa 3 — CVEs (vía OSV.dev, no se almacenan)

codefit **no mantiene una base de CVEs propia**. En cada scan consulta **OSV.dev** (gratis, sin API key) con las dependencias y sus versiones exactas. OSV agrega GitHub Advisory Database, bases de distros Linux y otros ecosistemas, y se actualiza globalmente en tiempo real. codefit se para sobre infraestructura que el mundo entero ya mantiene.

### Bootstrap con LLM (futuro)

Para mantener los knowledge packs, una optimización futura: usar la propia capacidad del agente para generar un borrador del pack a partir del changelog de una nueva versión de framework, que un humano revisa y aprueba. Usar la IA para mantener la herramienta que audita IA. No es parte del MVP.

---

## 19. Optimización de rendimiento y tokens

El objetivo: que codefit funcione a la perfección en modo MCP sin degradarse, minimizando el trabajo determinístico y la cantidad de superficie que el agente tiene que razonar (que es lo que consume tokens del agente). Las justificaciones cuantitativas viven en un documento de análisis paralelo.

### Principio rector: pirámide de filtrado

**Nunca enviar al agente para razonar lo que una capa más barata puede resolver con certeza.**

| Capa | Costo | Qué resuelve |
|---|---|---|
| 0 — Filtro de cambios | Gratis | Archivos sin cambios (since / hash de caché) |
| 1 — Regex / patrones | Microsegundos | Secretos, patrones concluyentes |
| 2 — AST + reglas + superficie | Sub-ms | Determinístico concluyente + enumera superficie |
| 3 — Razonamiento | Tokens del agente | Solo la superficie que las capas 0-2 no pudieron concluir |

La superficie que codefit entrega al agente es completa (no hay punto ciego), pero solo incluye lo que requiere razonamiento — nunca lo ya dictaminado por el AST. Esto minimiza los tokens que el agente gasta.

### Optimizaciones (requisitos de diseño)

1. **Pirámide de filtrado** — descrita arriba. Mayor impacto.
2. **Caché de findings por hash de contenido** — si el archivo no cambió, se reutilizan sus findings desde `.codefit/cache/`. En el flujo diario, un `codefit-scan-all` recurrente cuesta casi como un incremental.
3. **Mapeo de superficie acotado** — el AST enumera superficie solo de las categorías relevantes presentes en el código; no enumera lo que no aplica.
4. **Lazy evaluation** — codefit nunca corre un sensor que no se pidió. Garantizado por el diseño stateless.
5. **Resultados parciales / cancelación temprana** — si `codefit-scan-security` encuentra un crítico bloqueante, el agente puede decidir no llamar más tools sobre código que será reescrito.
6. **Orden consciente del costo** — los sensores gratuitos corren primero; el `blocked` puede determinarse antes de mapear superficie innecesaria.

### Garantía de no-degradación al escalar lenguajes

Las optimizaciones viven en el núcleo (`core/pipeline`, `core/cache`, `core/surface`), no en los providers. Un lenguaje nuevo hereda automáticamente toda la pirámide, el caché y el mapeo de superficie sin reimplementar nada. El lenguaje #5 es tan eficiente como el #1.
---

## 20. Sensores — Especificación detallada

Las reglas completas están en RF-01 a RF-10. Detalles de implementación:

### Sensor de Seguridad
- **Capa 1 (sin razonamiento):** regex de alta precisión para secretos hardcodeados. < 1 segundo.
- **Capa 2 (AST):** reglas formato-Semgrep para inyección, XSS directo, crypto débil, config insegura; y mapeo de superficie para IDOR, authz, over-fetching.
- **Capa 3 (razonamiento):** la ejecuta el agente sobre el `surface` devuelto. codefit no la ejecuta.
- **Dependencias:** OSV.dev.

### Sensor DB
- Lee `schema.prisma`, `*.sql`, anotaciones TypeORM/Hibernate, modelos SQLAlchemy (según provider).
- Detección automática OLTP vs OLAP.
- Las reglas de índices requieren schema + análisis de queries del código.

### Sensor de Tests — riesgo de regresión
En modo incremental produce un reporte adicional de qué puede romperse: funciones públicas modificadas y sus callsites sin cobertura, utilities compartidas modificadas, cambios de schema que impactan queries. No ejecuta tests.

### Sensor de Code Review
Combina los hallazgos determinísticos con la superficie, y entrega al agente el conjunto con contexto del proyecto. El agente razona como un senior. Es de los sensores más beneficiados por el modelo MCP-first: el razonamiento lo pone el LLM del agente (a menudo tan bueno o mejor que el que usan internamente los AI reviewers comerciales), y codefit le da mejor contexto (superficie estructural completa, no un diff aislado).

---

## 21. Sistema de reporte

### JSON canónico

```json
{
  "schema_version": "1.0",
  "codefit_version": "0.1.0",
  "timestamp": "2026-06-18T15:30:00Z",
  "project": "plantalinda-api",
  "language": "typescript",
  "commit": "abc1234",
  "score": {
    "global": 64,
    "by_dimension": {
      "security": 41, "review": 72, "db": 58,
      "complexity": null, "tests": 71
    }
  },
  "blocked": true,
  "block_reason": "Hallazgos críticos de seguridad sin consentimiento explícito",
  "baseline": { "active": true, "new_findings": 3, "baselined_findings": 47 },
  "findings": [ ... ],
  "surface": [ ... ],
  "coverage_note": "No auditado: race conditions, fallas de diseño arquitectónico.",
  "sensor_results": [ ... ]
}
```

### schema_version

El JSON declara su `schema_version` independiente de la versión de codefit, para que los consumidores (dashboards, integraciones de comunidad) no se rompan cuando el formato evolucione. Requisito desde Fase 0.

### Dimensiones no medidas vs medidas sin findings

Una dimensión sin sensor activo se reporta como **no medida** (`null`), NO como 100/100. El render distingue "— (no medido)" de "100 (auditado, sin findings)". El score global se calcula solo sobre las dimensiones medidas, re-normalizando los pesos. Esto evita la falsa sensación de completitud y refuerza el principio de honestidad.

### coverage_note

Todo reporte incluye una nota de cobertura (derivada del manifiesto) que aclara qué clases de problemas no fueron auditadas. Materializa "siempre se informan las consecuencias" en el reporte mismo.

### scan-all: síntesis de tres buckets sobre el JSON canónico

El JSON canónico de arriba es el **reporte completo** (score, dimensiones, findings) — el modelo de
destino. La tool `codefit-scan-all` **no** devuelve ese dump entero: entrega la **síntesis de tres
buckets** (`actionable` / `resolved_clean` / `frontier_pending`, ver sección 11), porque el dump
completo truncaba en los clientes MCP. El detalle de cualquier endpoint nombrado queda a una llamada
de `codefit-scan-endpoint` (ADR 0006/0008). El score, el baseline y las dimensiones del JSON canónico
son el modelo que esa síntesis resume.

### blocked

Si hay críticos de seguridad sin consentimiento, `blocked: true`. Los `baselined: true` no activan el bloqueo. codefit **informa**; la conducta de bloqueo es política del dev (codefit no toca el git — ver sección 13).

### Renderers

El JSON es la fuente de verdad. El núcleo es agnóstico al renderer. En v1, el reporte se entrega como JSON al agente (que lo presenta al developer en lenguaje natural). Un renderer HTML standalone es posible a futuro (sección 27), sin tocar el núcleo.

### Score global

```
score_global = Σ (score_dimensión × peso_dimensión) / Σ pesos_de_dimensiones_medidas
```
Pesos configurables; defaults: security 35, review 20, db 20, complexity 15, tests 10.

---

## 22. Soporte de plataformas y lenguajes

### Plataformas (day 1)

| OS | Arq. | Soporte |
|---|---|---|
| Linux | x86_64 | ✅ target primario |
| Linux | arm64 | ✅ cross-compile |
| Windows | x86_64 | ✅ binario nativo |
| Windows | WSL2 | ✅ como Linux |
| macOS | arm64 | 🔶 incluido en goreleaser, no testeado activamente en v1 |

### Lenguajes por fase

El versionado sigue SemVer con Fase→MINOR (ver sección 25 y `VERSIONING.md`).

| Lenguaje | Fase / versión | Nota |
|---|---|---|
| **Go** | Fase 0 (`0.1.0`) | Provider de arranque: codefit se audita a sí mismo desde el primer commit |
| TypeScript / React / Next / Express / Fastify / NestJS | Fase 1 (`0.1.x`) | Primer target de producto completo; la superficie (IDOR/authz/over-fetching) cubre Next.js App Router + Server Actions, Express, Fastify y NestJS (rutas por decoradores), con acceso cross-file señalado, no seguido — opción C |
| Java / Spring | post-`1.0.0` (`1.1`) | Solo el provider; el núcleo no cambia |
| Python / FastAPI / Django | post-`1.0.0` (`1.2`) | Idem |

Cada lenguaje nuevo valida el diseño de extensibilidad: si agregar Java requiere tocar el núcleo, el diseño falló.

### Bases de datos (v1.0)

| DB | OLTP | OLAP | Schema source | ORM |
|---|---|---|---|---|
| PostgreSQL | ✅ | ✅ | .sql, prisma | Prisma, TypeORM, Drizzle |
| SQLite | ✅ | — | .sql, prisma | Prisma |
| MySQL | ✅ | — | .sql | TypeORM, Prisma |

---

## 23. Posicionamiento competitivo

El mercado de auditoría de código se partió en dos campos que se complementan y que los equipos serios combinan: AI PR reviewers (LLM puro) y plataformas de verificación (estático determinístico + IA). codefit se ubica en una tercera posición habilitada por MCP.

### Frente a AI PR reviewers (CodeRabbit, Greptile, Qodo)

Estas viven en el PR, en la nube, después de escribir el código. CodeRabbit (gratis para OSS, $12–24/mes por dev) es fuerte en amplitud de workflow, no en profundidad de review. Su techo, reconocido por el mercado, es el retrieval: cuando solo ven "el diff más 100 líneas alrededor", todos regresan al mismo límite.

**Valor agregado de codefit:** actúa *durante* la generación, dentro del agente, antes del commit. Y ataca el techo de retrieval con el mapeo de superficie completa: no le da al razonador un diff, le da toda la superficie estructural relevante con contexto del proyecto.

### Frente a SonarQube (verificación + quality gate)

Estándar enterprise: 6500+ reglas determinísticas, 35+ lenguajes, auditabilidad total (cada hallazgo trazable a una regla), y el killer feature del quality gate que bloquea PRs. Su techo: maneja patrones conocidos mejor que fallas desconocidas; no razona sobre supuestos ocultos; no es para equipos de menos de ~20 personas por costo y ops self-hosted.

**Valor agregado de codefit:** el quality gate de Sonar es su modelo de bloqueo, pero Sonar lo pone en el CI/CD enterprise; codefit lo pone en el loop del agente, local, gratis, sin infraestructura. codefit es "el Sonar para el solo dev y el equipo chico que codea con IA". Además, Sonar ya validó la dirección: su servidor MCP expone findings a los agentes — pero como add-on de su plataforma paga. codefit nace MCP-first y open source.

### Frente a Semgrep / Snyk (SAST / seguridad)

Motores open source potentes en escaneo de seguridad y taint analysis. Su límite: no son para bugs sutiles de producto ni supuestos ocultos. codefit adopta su formato de reglas (sección 17) pero reemplaza el taint estático por razonamiento sobre superficie.

### Frente a equipos humanos

Siguen existiendo, con el rol cambiado: la IA maneja lo mecánico (sintaxis, patrones, escaneos), los humanos manejan diseño, trade-offs arquitectónicos y lógica de negocio. El problema que todos citan: la generación con IA escaló la velocidad pero no la capacidad de review. **Valor agregado de codefit:** no reemplaza al humano —lo libera de lo mecánico para que use su tiempo en lo que solo el humano hace.

### Resumen honesto

**Donde codefit gana:** dev solo o equipo chico que codea con IA, MCP-first nativo, gratis y open source, sin infraestructura, bloqueo en el loop del agente antes del commit, y mapeo de superficie completa contra el techo de retrieval.

**Donde el mercado lleva ventaja hoy (y hay que ser honesto):** las maduras tienen miles de reglas curadas por años, integración nativa multi-plataforma, bases de falsos positivos afinadas, y taint estático real (que codefit reemplaza por razonamiento, mejor en muchos casos pero no determinístico). Dato de calibración: el líder de AI review alcanza ~46% de accuracy en bugs de runtime reales — recordatorio de que el manifiesto de cobertura honesto es un diferenciador, no una debilidad.

---

## 24. Complementariedad con SDD y TDD

codefit no compite con SDD ni TDD: cubre un agujero que ninguna de las dos tapa.

```
SDD     → ¿construí lo correcto?        (intención vs implementación)
TDD     → ¿funciona como espero?         (comportamiento verificado)
codefit → ¿está bien hecho y es seguro?  (lo que no se ve)
```

### Cómo se usa SDD hoy y qué agrega codefit

SDD garantiza que el código *implemente lo que se especificó*: se escribe una spec, el agente genera contra ella, se verifica que cumple los requerimientos. **Su agujero:** verifica *qué* se construyó, no la calidad interna. Una spec puede cumplirse perfectamente con código O(n²), con un IDOR y con una FK sin índice. **codefit** audita esa dimensión que SDD no toca: SDD garantiza que hiciste lo correcto; codefit, que lo hiciste bien. En el ciclo SDD, codefit entra como fase posterior a la implementación.

### Cómo se usa TDD hoy y qué agrega codefit

TDD garantiza que el código *se comporta como se espera*: test primero, implementación mínima, refactor. **Su agujero:** verifica el comportamiento *que el dev pensó testear*. Un test pasa con un secreto hardcodeado, con un algoritmo exponencial (si usa pocos datos), con un IDOR (si no simula un atacante). El código generado por IA suele tener tests de happy path. **codefit** detecta las clases de problemas que el dev no sabe que tiene que buscar. En regresión se complementan: TDD ejecuta los tests; codefit evalúa si la suite es suficiente y qué riesgo de regresión introduce un cambio.

### La foto de las tres juntas

Un dev que usa las tres tiene cubierto el qué (SDD), el cómo-se-comporta (TDD) y el cómo-está-hecho (codefit). Ninguna puede hacer el trabajo de las otras dos. codefit las completa.
---

## 25. Rollout por fases

### Versionado (SemVer, Fase→MINOR)

codefit sigue **SemVer 2.0 con pre-releases**, mapeado a estas fases (contrato en `VERSIONING.md`).
Mientras es pre-estable se queda en `0.x`; cada fase que **cierra** sube el MINOR, y el MINOR aterriza
**sin sufijo solo cuando la fase está completa y usable end-to-end desde `main`** (misma regla de
honestidad que README/CHANGELOG: no se anuncia una fase como hecha con piezas en stub).

| Versión | Fase | Significado |
|---|---|---|
| `0.1.0` | Fase 1 | TS provider + sensor de seguridad + mapeo de superficie + `init`, baseline funcionales |
| `0.2.0` | Fase 2 | Sensor de DB |
| `0.3.0` | Fase 3 | Code review + best practices + tests |
| `0.4.0` | Fase 4 | Knowledge packs + manifiesto + release pública `0.x` |
| `1.0.0` | — | API estable; post-1.0 trae Java (`1.1`), Python (`1.2`) |

**Estado actual: `v0.1.0` — Fase 1 COMPLETA.** El core MCP dogfoodeado (detección de seguridad TS,
las tres categorías de superficie, el resumen de tres buckets de `scan-all` con `scan-endpoint`
on-demand, todo sobre stdio), **`codefit init`** (config + skill) y el **baseline** (memoria del
proyecto: `scan-all` con delta + `baseline-list`/`-accept`/`-prune`). Validado en uso real sobre un
backend Next.js/Prisma. (`codefit update` es Fase 4, no Fase 1.)

### Fase 0 — Foundations + Núcleo + Go Provider ✅ `(~2 semanas)`
- Repo Go, estructura de tres capas (`core/`, `sensors/`, `providers/`), CI propio.
- Interface `Sensor`, `LanguageProvider`, tipos base (`Finding` con `reasoning`/`baselined`, `SurfaceItem`, `ConsentRecord`).
- **Go `LanguageProvider`** (arranque): `go/ast`, queries base. Permite el self-audit inmediato.
- tree-sitter puro Go (sin CGO) verificado con cross-compile (la integración para TS se ejercita en Fase 1).
- Núcleo: `pipeline` (capas 0-2), `surface` (framework de mapeo), `cache`, `scoring`, `baseline`, `report` (JSON con `schema_version`, dimensiones no-medidas, `coverage_note`), `ruleengine` (matcher subset Semgrep), `cve` (OSV.dev), `coverage`.
- Soporte de `path_criticality`.
- Plumbing CLI: `mcp serve` (skeleton), `init`, `update` (skeleton), `status`, `version`.
- Parser y validador de `.codefit.yaml`.
- Self-audit como **test de integración Go** (ver sección 26) + test de integración MCP.

**Done cuando:** binario único compila sin CGO y cross-compila a Windows. El Go provider implementa `LanguageProvider`. El self-audit corre como `go test` y no hay críticos en el código de codefit. La estructura núcleo+providers está sólida.

### Fase 1 — TypeScript Provider + Sensor de Seguridad + MCP ✅ **COMPLETA** `(~3 semanas)` *(prioridad máxima)*
- ✅ **TypeScript `LanguageProvider`**: `gotreesitter` (puro Go), reglas formato-Semgrep, defaults de criticidad.
- ✅ Sensor de seguridad: capa 1 (secretos), capa 2 (reglas + **mapeo de superficie** IDOR/authz/overfetch) con tres niveles de certeza.
- ✅ Consentimiento explícito para critical security; `path_criticality` activo.
- ✅ **MCP server funcional (SDK oficial, stdio)**: `codefit-scan-security`, `codefit-surface-idor/authz/overfetch`, `codefit-confirm-surface`, `codefit-scan-all`, `codefit-scan-endpoint`, `codefit-coverage`, `codefit-check-cves`.
- ✅ **`codefit init`**: genera `.codefit.yaml` + la skill de codefit y la coloca para los agentes detectados (no toca el `AGENT.md`). Dogfoodeado en un backend real; trigger de la skill validado en ambos sentidos.
- ✅ **Baseline (RF-08)**: `.codefit-baseline` commiteado, identidad por contenido, delta en `scan-all`, salvaguarda graduada por certeza, y `baseline-list`/`-accept`/`-prune` (ADR 0009–0012). Dogfoodeado sobre Bitácora.
- ✅ **CVEs vía OSV.dev (RF-09)**: `codefit-check-cves` lee versiones exactas de los lockfiles (`package-lock.json`) y `go.mod`, consulta OSV.dev (gratis, sin API key) y reporta las dependencias vulnerables con id, severidad (la que da OSV — codefit no recomputa CVSS), versión corregida y referencias. Sin lockfile no adivina: lo informa como nota. Validado contra OSV real (npm + Go).

**Done cuando:** ✅ un agente (OpenCode/Claude Code) corre `codefit-scan-all`, recibe el resumen de tres buckets, sigue un `frontier_pending` con `codefit-scan-endpoint`, razona la superficie con su LLM, y el baseline permite adoptar un proyecto existente sin ruido (re-scan silencia lo `known`). Detecta ≥3 categorías de vulnerabilidades con cero falsos positivos en secretos. Corre sobre Go (self-audit) y TypeScript. **Fase 1 cerrada → se cuta `v0.1.0`.**

### Fase 2 — Sensor de DB `(~2 semanas)`
- `ParseSchema` del TS provider (`schema.prisma`, SQL).
- Reglas OLTP (FKs, índices, vistas, procs, triggers) + detección OLAP + reglas OLAP básicas.
- N+1 (RF-04). Violaciones 3FN candidatas como superficie a razonar.
- Tool `codefit-scan-db`.

**Done cuando:** `codefit-scan-db` sobre PlantaLinda genera hallazgos reales verificados.

### Fase 3 — Code Review + Best Practices + Tests `(~2 semanas)`
- `codefit-review-code` (combina determinístico + superficie), `codefit-check-practices`, `codefit-scan-tests`.
- Riesgo de regresión en modo incremental.
- Integración de hallazgos confirmados por el agente al reporte (protocolo de cierre).

**Done cuando:** `codefit-review-code` produce un review accionable en un PR real de PlantaLinda, con la superficie razonada por el agente integrada al reporte.

### Fase 4 — Conocimiento + Release público `(~2 semanas)`
- Knowledge packs (repo `codefit-cli/knowledge`) + `codefit update` funcional.
- Manifiesto de cobertura: `COVERAGE.md` + tool `codefit-coverage` desde fuente única.
- goreleaser: binarios Linux x86_64/arm64, Windows x86_64.
- README, comunidad (CONTRIBUTING, SECURITY, CODE_OF_CONDUCT, templates).
- Licencia Apache 2.0, tag **`v0.4.0`** (cierre de Fase 4; ver tabla de versionado arriba).

**Done cuando:** un usuario externo instala codefit, lo conecta como MCP a su agente, y audita su proyecto TypeScript en los tres escenarios.

### Post-v1.0 — Escalado de lenguajes
- **v1.1:** Java `LanguageProvider`. Solo el provider; el núcleo no cambia.
- **v1.2:** Python `LanguageProvider`. Idem.

Nota sobre complejidad algorítmica: el sensor de complejidad empírica (benchmarking en sandbox Docker) está contemplado en el modelo de scoring (dimensión `complexity`) pero su implementación completa se evalúa post-v1.0, dado que requiere ejecución de código y no encaja en el flujo MCP determinístico. Hasta entonces, la dimensión `complexity` se reporta como no medida.

---

## 26. Self-audit y dogfooding

codefit se audita a sí mismo desde el primer commit, usando el Go provider. En un modelo MCP puro (sin CLI de auditoría), el self-audit se resuelve como **test de integración Go**, no como un scan de terminal.

### Mecanismo

```
En cada PR, el CI corre go test ./..., que incluye:
  • Tests unitarios de cada sensor.
  • Test de self-audit: corre los sensores (vía su API interna de Go) sobre el
    propio código de codefit y asegura que no haya findings críticos.
  • Test de integración MCP: levanta el server, llama una tool, verifica la
    respuesta — valida la capa de transporte.
```

### Por qué como test y no como scan

El self-audit conceptualmente es una aserción: *dado el código de codefit, cuando corro el sensor de seguridad, entonces no hay críticos*. Eso encaja naturalmente como test Go. No reintroduce el CLI de auditoría, no agrega dependencias externas, y el self-dogfooding sigue vivo en cada PR como parte de `go test`. La capa MCP se valida por separado con su propio test de integración.

### Valor

Cada falso positivo que codefit encuentra en su propio código se convierte en una corrección inmediata. Es el mejor caso de prueba —real, permanente, gratis— y un argumento de marketing: "la herramienta que se audita a sí misma". A medida que el proyecto madura, el reporte de auto-auditoría se publica como señal de confianza.

---

## 27. Roadmap futuro / Ideas

Funcionalidades aprobadas conceptualmente, diferidas a post-v1.0. No afectan la arquitectura del núcleo (se agregan como capas sobre el JSON canónico, como providers, o como sensores), por lo que se difieren sin riesgo ni deuda.

### Sensor de complejidad algorítmica empírica
Benchmarking en sandbox Docker con entradas crecientes y regresión de curvas (O(1)…O(2ⁿ)). Requiere ejecución de código, fuera del flujo MCP determinístico. Se evalúa cómo integrarlo (¿un comando de plumbing que ejecuta benchmarks? ¿una tool MCP que dispara el sandbox?) sin violar el principio MCP-first.

### Renderer HTML standalone
Reporte visual compartible sobre el JSON canónico. No toca el núcleo.

### Self-audit publicado
Publicar el reporte de auto-auditoría como señal de calidad ("the tool that audits itself").

### Bootstrap de knowledge packs con LLM
Generar borradores de packs desde changelogs de frameworks, revisados por humanos.

### Otras ideas en evaluación
- Renderer SARIF para integración con GitHub Code Scanning.
- Dashboard web que consume reportes JSON en el tiempo (evolución del score).

### Explícitamente descartado
- **Modo CLI de auditoría con LLM propio** (los 4 backends, wizard, delegación a CLIs locales). Se descartó deliberadamente para mantener codefit MCP-first puro, sin deuda. La auditoría ocurre siempre vía el agente.

---

## 28. Métricas de éxito

### Calidad del producto
- Falsos positivos en detección de secretos hardcodeados: **< 1%**.
- Falsos positivos en reglas determinísticas de DB: **0%**.
- Completitud del mapeo de superficie vs. análisis manual: **> 90%** de la superficie relevante enumerada.
- Tiempo de una llamada `codefit-scan-security` (determinístico) en proyecto mediano: **< 10 segundos**.

### Adopción
- GitHub stars en el primer mes.
- Proyectos con `.codefit.yaml` commiteado (GitHub code search).
- Agentes/clientes MCP que integran codefit.
- Issues y PRs de comunidad (reglas, providers, knowledge packs).

### Dogfooding
- codefit se auto-audita en cada PR desde Fase 0.
- Cualquier hallazgo en PlantaLinda no detectado manualmente = validación de valor.

---

## 29. Decisiones resueltas

| # | Decisión | Resolución |
|---|---|---|
| 1 | Nombre / proyecto | ✅ `codefit` |
| 2 | Licencia | ✅ Apache 2.0 |
| 3 | Repo / org | ✅ `github.com/codefit-cli/codefit` |
| 4 | Distribución | ✅ GitHub Releases via goreleaser |
| 5 | Lenguaje de implementación | ✅ Go (binario único, comunidad; bottleneck es razonamiento del agente, no CPU) |
| 6 | Parsing | ✅ `go/ast` para Go; tree-sitter puro Go (sin CGO) para el resto |
| 7 | Arquitectura | ✅ Núcleo universal + LanguageProvider |
| 8 | **Modelo de operación** | ✅ **MCP-first puro. Sin modo CLI de auditoría.** |
| 9 | **LLM** | ✅ **codefit no gestiona LLM. El agente aporta el razonamiento.** |
| 10 | Estado | ✅ Stateless |
| 11 | Transport MCP | ✅ SDK oficial `modelcontextprotocol/go-sdk` (ADR 0007); **solo stdio**, HTTP/SSE diferido |
| 12 | Anti-punto-ciego | ✅ Mapeo de superficie completa (no quirúrgico) |
| 13 | Cobertura | ✅ Manifiesto explícito: doc (`COVERAGE.md`) + tool (`codefit-coverage`), fuente única |
| 14 | Motor de reglas | ✅ Formato Semgrep (subset core) + matcher propio en Go (sin OCaml) |
| 15 | Taint analysis | ✅ No determinístico; se cubre por razonamiento sobre superficie |
| 16 | CVEs | ✅ OSV.dev (gratis, sin key) |
| 17 | Actualización de conocimiento | ✅ 3 capas: reglas (binario) / knowledge packs (`codefit update`) / CVEs (OSV en cada scan) |
| 18 | Provider de arranque | ✅ Go, desde Fase 0 (self-audit) |
| 19 | Self-audit en MCP puro | ✅ Test de integración Go + test de integración MCP |
| 20 | Adopción de proyectos existentes | ✅ Baseline (RF-08) |
| 21 | Severidad contextual | ✅ path_criticality (RF-10) |
| 22 | Compatibilidad del JSON | ✅ schema_version desde Fase 0 |
| 23 | Explicabilidad | ✅ Campo `reasoning` en findings razonados por el agente |
| 24 | Bloqueo de commit | ✅ codefit informa `blocked`; **no toca el git** — la conducta es política del dev |
| 25 | Onboarding | ✅ codefit genera su **propia skill** y la coloca por agente detectado; **no toca el `AGENT.md`** del usuario |
| 26 | Autonomía | ✅ Principio transversal: el developer siempre decide; se informan consecuencias |
| 27 | Nombres de tools | ✅ `codefit-` + guión medio; familia `codefit-surface-*` para mapeo |
| 28 | Parser TS | ✅ `gotreesitter` puro Go sin CGO (ADR 0002); frontera AST vía `core/syntax.Node` (ADR 0003) |
| 29 | Frontera rule/superficie | ✅ Regla = local y concluyente; superficie = seguir el dato (ADR 0004). Sin `mode: taint` |
| 30 | Frontera del mapeo | ✅ Finito (enumerado) / infinito (handoff al agente, wording afirmativo) / no cubierto (declarado) (ADR 0005) |
| 31 | Certeza | ✅ Tres niveles: `deterministic` / `surface_confirmed` / `surface_frontier` + campo-hecho `StructuralFacts` (ADR 0005/0006) |
| 32 | Reporte de `scan-all` | ✅ Resumen de tres buckets + detalle on-demand `codefit-scan-endpoint` (ADR 0006/0008) |
| 33 | Versionado | ✅ SemVer, Fase→MINOR; **`v0.1.0` = Fase 1 completa** (`VERSIONING.md`) |
| 34 | Identidad del baseline | ✅ Por **contenido** (categoría+file+snippet, sin línea); contenido hasheado, nunca guardado (ADR 0009) |
| 35 | Modelo del baseline | ✅ Foto del **estado actual** (delta new/changed/known/gone), no lista de aceptados; `gone` retenido hasta `prune` (ADR 0010) |
| 36 | Salvaguarda graduada | ✅ Superficie (pregunta) → known auto; determinístico (afirmación 1.0) → nunca auto-silenciado, accept humano explícito (ADR 0011) |
| 37 | Operación del baseline | ✅ Tools que opera el agente vía skill (`list`/`accept`/`prune`); el humano decide; codefit nunca toca el código (ADR 0012) |

---

## 30. GitHub y open source

### Identidad

| Atributo | Valor |
|---|---|
| Organización | `github.com/codefit-cli` |
| Repo principal | `github.com/codefit-cli/codefit` |
| Repo de conocimiento | `github.com/codefit-cli/knowledge` |
| Módulo Go | `github.com/codefit-cli/codefit` |
| Licencia | Apache 2.0 |
| Binario | `codefit` |
| Config | `.codefit.yaml` |
| Descripción | "The MCP-first auditor for AI-generated code: security risks, algorithmic complexity, DB quality & code review — codefit maps, the agent reasons." |

### Estructura de comunidad

```
.github/
  workflows/   ci.yml · release.yml · security.yml
  ISSUE_TEMPLATE/  bug_report · feature_request · false_positive · new_rule · new_provider
  PULL_REQUEST_TEMPLATE.md
CONTRIBUTING.md   # incluye: cómo agregar un LanguageProvider, cómo escribir reglas formato-Semgrep
CODE_OF_CONDUCT.md  SECURITY.md  CHANGELOG.md  LICENSE  README.md  COVERAGE.md
```

### CI (resumen)
En cada PR: `go test ./...` (unitarios + self-audit + integración MCP), `go vet`, `golangci-lint`, build sin CGO, cross-compile. Release via goreleaser en cada tag.

### Instalación para el usuario
```bash
# go install
go install github.com/codefit-cli/codefit/cmd/codefit@latest
# luego se conecta como MCP server en el agente (ver sección 11)
```

### Milestones

Según el versionado de la sección 25 (`VERSIONING.md`):

| Milestone | Contenido |
|---|---|
| `v0.1.0-alpha.x` | Pre-releases en el camino (alpha.1 core MCP, alpha.2 + `init`) |
| `v0.1.0` | **Fase 1 completa (actual)** — Núcleo + Go + TS + Security + MCP + `init` + baseline |
| `v0.2.0` | Fase 2 (DB) |
| `v0.3.0` | Fase 3 (Review + Practices + Tests) |
| `v0.4.0` | Fase 4 (Conocimiento + manifiesto) — primera release pública `0.x` |
| `v1.0.0` | API estable |
| `v1.1.0` / `v1.2.0` | Java provider / Python provider |

---

## 31. Glosario

| Término | Definición |
|---|---|
| **MCP** | Model Context Protocol. Estándar para exponer herramientas a agentes de IA. |
| **MCP-first** | codefit se opera exclusivamente como servidor MCP; no tiene modo CLI de auditoría. |
| **Orquestador / agente** | El agente de IA del developer (Claude Code, OpenCode, etc.) que llama a las tools de codefit y aporta el razonamiento LLM. No es parte de codefit. |
| **Plumbing CLI** | Los pocos comandos de terminal de codefit que no auditan (serve, init, update, status, version). |
| **Sensor** | Módulo del núcleo que produce findings + superficie para una dimensión. Agnóstico al lenguaje. |
| **Finding** | Hallazgo individual con severidad, dimensión, ubicación, recomendación. |
| **Mapeo de superficie** | Enumeración completa de la superficie estructural de una clase de vulnerabilidad, para que el agente razone sin puntos ciegos. |
| **SurfaceItem** | Un elemento de superficie (categoría, ubicación, snippet, hechos estructurales, razón a revisar) entregado al agente. |
| **Skill (de codefit)** | El `SKILL.md` propio que `codefit init` genera (Anthropic Agent Skills Spec) y coloca por agente; dispara el loop y apunta a las tools. codefit no toca el `AGENT.md` del usuario. |
| **StructuralFacts** | Hechos estructurales booleanos de un SurfaceItem (`local_access_detected`, `known_authz_detected`, `field_limiting_detected`) — base del orden y la clasificación, no juicios. |
| **Niveles de certeza** | `deterministic` (afirma, 1.0) / `surface_confirmed` (vio la forma local, pregunta) / `surface_frontier` (el dato salió del handler, pregunta). |
| **Tres buckets (scan-all)** | `actionable` (resuelto local con gap, detallado) / `resolved_clean` (resuelto local sin gap, nombrado + hecho de verificación) / `frontier_pending` (no concluido localmente, nombrado). |
| **Frontier / handoff** | El límite donde codefit deja de seguir el dato y se lo pasa al agente, con una señal que afirma el límite ("NOT verified here. Follow…"), nunca un falso "no detectado". |
| **scan-endpoint** | Tool que re-analiza un archivo on-demand y devuelve el detalle completo de un endpoint nombrado por `scan-all`. Stateless. |
| **Manifiesto de cobertura** | Declaración explícita de qué clases codefit cubre y cuáles no. Doc (`COVERAGE.md`) + tool (`codefit-coverage`). |
| **LanguageProvider** | Interface que cada lenguaje implementa. Permite escalar sin tocar el núcleo. |
| **Núcleo / core** | Capa universal agnóstica al lenguaje. |
| **Pirámide de filtrado** | Capas baratas (cambios, regex, AST) resuelven primero; solo la superficie no concluyente va al razonamiento del agente. |
| **Knowledge pack** | Conocimiento por versión de framework, actualizable con `codefit update` sin reinstalar el binario. |
| **Baseline** | Foto de findings que permite reportar solo lo nuevo e ignorar deuda histórica. |
| **path_criticality** | Clasificación de paths (production/test/example) que pondera severidad. |
| **ConsentRecord** | Registro de aceptación explícita de un crítico de seguridad. |
| **schema_version** | Versión del formato JSON, independiente de la de codefit. |
| **Self-audit** | codefit auditándose vía test de integración Go en cada PR. |
| **OSV.dev** | Base de CVEs abierta y gratuita que codefit consulta en cada scan. |
| **Formato Semgrep (subset)** | Sintaxis de reglas declarativas que el matcher propio de codefit interpreta (operadores core, sin taint). |
| **tree-sitter puro Go** | Parser incremental sin CGO, para los lenguajes que la stdlib no parsea. |
| **OLTP / OLAP** | Paradigmas transaccional / analítico de base de datos, con reglas de auditoría distintas. |
