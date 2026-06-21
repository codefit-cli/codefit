# PRD — codefit
**Product Requirements Document v1.3**
**Estado:** Final · **Autor:** Lucas (Architect) · **Fecha:** Junio 2026

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
13. [Enriquecimiento del AGENT.md](#13-enriquecimiento-del-agentmd)
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
- El bloqueo de commit por hallazgo crítico lo **configura el dev** al aceptar (o no) que codefit enriquezca el `AGENT.md`. codefit propone; el dev dispone.
- El consentimiento de seguridad (un crítico puede aceptarse) siempre **informa la consecuencia** y deja registro auditable.
- El enriquecimiento del `AGENT.md` se hace **con confirmación explícita** del dev, nunca en silencio.
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
- **Bloqueo en el loop del agente**, local, antes del commit — no en el CI/CD enterprise.
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
El agente reporta al developer / corrige / decide según el AGENT.md
```

### codefit NO es un subagente

codefit es un **servidor de herramientas**, no un subagente del orquestador.

```
┌─────────────────────────────────────────────────────┐
│              ORQUESTADOR (el agente del dev)         │
│  - Decide el flujo                                  │
│  - Llama herramientas MCP de codefit                │
│  - Razona la superficie con su propio LLM           │
│  - Decide qué hacer con los findings (según AGENT.md)│
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

- **stdio por defecto** (el caso estándar: el agente levanta codefit como subproceso local).
- **HTTP/SSE opcional** (`--port`) para clientes remotos.

---

## 8. Los tres escenarios de uso

codefit cubre tres situaciones distintas, todas vía MCP. La diferencia no es el mecanismo (siempre MCP), sino *cuándo* y *con qué granularidad* el agente llama a codefit.

### Escenario A — Desarrollo activo (el caso principal)

El developer está construyendo features con su agente. codefit se invoca automáticamente (vía las reglas del `AGENT.md`) en dos niveles:

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

### RF-11 · Inicialización y configuración

`codefit init` analiza el proyecto y genera `.codefit.yaml` detectando lenguaje, framework, ORM, paradigma de DB, y archivos de test. Si el dev lo autoriza, enriquece el `AGENT.md` con las reglas de orquestación de codefit (ver sección 13).

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

| Tool MCP | Tipo | Qué hace |
|---|---|---|
| `codefit-scan-security` | Determinístico + superficie | Seguridad sobre un path/diff |
| `codefit-scan-db` | Determinístico | Estructura de DB (OLTP/OLAP, índices, vistas, procs) |
| `codefit-check-cves` | Determinístico | Consulta OSV.dev por las dependencias |
| `codefit-check-practices` | Determinístico | Best practices del lenguaje |
| `codefit-scan-tests` | Determinístico | Calidad de suite + riesgo de regresión |
| `codefit-surface-idor` | Superficie | Enumera endpoints con ID para razonar IDOR |
| `codefit-surface-authz` | Superficie | Enumera handlers protegibles |
| `codefit-surface-overfetch` | Superficie | Enumera serializaciones de datos |
| `codefit-review-code` | Combinada | Code review: determinístico + superficie, razonado por el agente |
| `codefit-scan-all` | Orquestadora | Todas las anteriores; foto completa (Escenarios B y C) |
| `codefit-baseline` | Utilidad | Toma/actualiza la línea base |
| `codefit-coverage` | Metadata | Devuelve el manifiesto de cobertura para el lenguaje |

Las `codefit-surface-*` son una familia conceptualmente distinta: no detectan, **enumeran superficie** para que el agente razone. Por eso llevan el segundo nivel `surface`.

### Contrato de respuesta

Toda tool devuelve el mismo shape (subset del reporte canónico):

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
      "reason_to_review": "Endpoint accede a recurso por ID; verificar ownership."
    }
  ],
  "summary": { "critical": 1, "high": 0, "medium": 0, "low": 0, "info": 0 },
  "blocked": true
}
```

El campo `surface` es la lista de elementos que el agente debe razonar. El campo `blocked: true` indica que hay críticos de seguridad sin consentimiento; el agente decide qué hacer según el `AGENT.md`.

### Cómo el agente devuelve hallazgos de superficie

Cuando el agente razona la superficie y encuentra una vulnerabilidad real, ese hallazgo debe poder integrarse al reporte canónico para baseline, score y trazabilidad. La tool `codefit-review-code` (y `codefit-scan-all`) aceptan un parámetro opcional de "hallazgos confirmados por el agente" en una llamada de cierre, que codefit integra al reporte marcándolos como `probabilistic: true` con el `reasoning` provisto por el agente. Así el razonamiento del agente queda registrado, no solo en la conversación. *(Detalle de protocolo a refinar en implementación de Fase 4.)*

### Configuración en el cliente agente

**Claude Code / Claude Desktop:**
```json
{
  "mcpServers": {
    "codefit": { "command": "codefit", "args": ["mcp", "serve"], "cwd": "/path/to/project" }
  }
}
```

**OpenCode:**
```json
{
  "mcp": {
    "codefit": { "type": "local", "command": ["codefit", "mcp", "serve"], "enabled": true }
  }
}
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
| `codefit mcp serve` | Levanta el servidor MCP (stdio; `--port` para HTTP/SSE). La razón de ser del binario. |
| `codefit init` | Genera `.codefit.yaml`; opcionalmente (con confirmación) enriquece el `AGENT.md`. |
| `codefit update` | Descarga los últimos knowledge packs (ver sección 18). |
| `codefit status` | Diagnóstico: versión, `.codefit.yaml` encontrado, knowledge packs instalados, conectividad OSV.dev. |
| `codefit version` | Versión, commit y fecha de build. |

No hay `codefit scan`, `codefit review` ni `codefit bench` como comandos de terminal. Esa funcionalidad vive exclusivamente en las herramientas MCP.

---

## 13. Enriquecimiento del AGENT.md

El mecanismo por el cual codefit se integra al flujo del agente —y por el cual se materializa el bloqueo de commit— es el enriquecimiento del archivo de contexto del agente (`AGENT.md`, `CLAUDE.md`, o el que corresponda al agente).

### Cómo funciona

Igual que herramientas como gentle-ai inyectan reglas de TDD/SDD, codefit puede inyectar sus reglas de orquestación en el `AGENT.md`. Esto instruye al agente sobre cuándo llamar a codefit y qué hacer con los resultados:

```markdown
## codefit (auditoría)
- Después de implementar cada función/feature y ANTES de proponer el commit,
  llamá a las herramientas de codefit MCP sobre el código nuevo/modificado.
- Empezá por codefit-scan-security. Si hay un finding CRÍTICO sin consentimiento,
  NO commitees: corregí y volvé a auditar.
- Para las clases de superficie (codefit-surface-*), razoná cada elemento y
  reportá al developer los que sean vulnerabilidades reales.
- Después de reportar, consultá codefit-coverage y aclará al developer qué
  clases de problemas NO fueron auditadas.
```

### El bloqueo: codefit informa, el AGENT.md define la conducta

El bloqueo de commit por crítico es **doble**:
- **codefit informa:** devuelve `blocked: true` con el detalle del finding y la consecuencia.
- **El AGENT.md define la conducta:** las reglas inyectadas le dicen al agente que ante un `blocked: true` no debe avanzar al commit.

codefit nunca bloquea por sí mismo un commit (no tiene poder sobre el git del usuario); provee la información, y la conducta de bloqueo la ejecuta el agente siguiendo las reglas que el developer aceptó.

### Autonomía del developer (principio innegociable)

- codefit **modifica el `AGENT.md` solo con confirmación explícita** del developer durante `codefit init`. Nunca lo toca en silencio.
- El developer puede rechazar el enriquecimiento y pegar las reglas a mano, o no usarlas.
- El developer puede ajustar el nivel de bloqueo (qué severidad bloquea, qué se informa sin bloquear).
- En todos los casos, codefit informa las consecuencias de cada opción.

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
---

## 15. Arquitectura técnica

### Stack

| Componente | Tecnología | Justificación |
|---|---|---|
| Núcleo / MCP server / plumbing CLI | Go | Binario único sin runtime, cross-compile limpio, concurrencia nativa, baja barrera de contribución, goreleaser |
| Parsing Go | `go/ast` (stdlib) | Parser oficial del lenguaje, sin dependencias, sin CGO |
| Parsing TS/Java/Python | tree-sitter **puro Go, sin CGO** | Preserva el binario único y el cross-compile; rápido en parsing incremental |
| Motor de reglas | Matcher propio (subset formato Semgrep) | Reglas portables y conocidas por la comunidad, sin embeber OCaml (ver sección 17) |
| MCP server | Go (mismo binario) | `codefit mcp serve` expone las tools sin proceso separado |
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
│   ├── core/             # ── NÚCLEO UNIVERSAL (agnóstico al lenguaje) ──
│   │   ├── context/      # AuditContext
│   │   ├── orchestrator/ # ejecución de sensores
│   │   ├── pipeline/     # pirámide de filtrado (capas 0-2)
│   │   ├── surface/      # framework de mapeo de superficie
│   │   ├── cache/        # caché de findings por hash
│   │   ├── scoring/      # scores por dimensión y global
│   │   ├── baseline/     # foto y diff de findings
│   │   ├── report/       # JSON canónico → renderers
│   │   ├── findings/     # Finding, Severity, Dimension, ConsentRecord, Surface
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
    Category       string   // "idor" | "authz" | "overfetch" | ...
    File           string
    Line           int
    Snippet        string
    ReasonToReview string
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

### blocked

Si hay críticos de seguridad sin consentimiento, `blocked: true`. Los `baselined: true` no activan el bloqueo. codefit informa; la conducta de bloqueo la ejecuta el agente vía AGENT.md.

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

| Lenguaje | Fase | Nota |
|---|---|---|
| **Go** | v1.0 (Fase 0) | Provider de arranque: codefit se audita a sí mismo desde el primer commit |
| TypeScript / React / Next | v1.0 | Primer target de producto completo |
| Java / Spring | v1.1 | Solo el provider; el núcleo no cambia |
| Python / FastAPI / Django | v1.2 | Idem |

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

### Fase 0 — Foundations + Núcleo + Go Provider `(~2 semanas)`
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

### Fase 1 — TypeScript Provider + Sensor de Seguridad + MCP `(~3 semanas)` *(prioridad máxima)*
- **TypeScript `LanguageProvider`**: tree-sitter puro Go, reglas formato-Semgrep, defaults de criticidad.
- Sensor de seguridad completo: capa 1 (secretos), capa 2 (reglas + **mapeo de superficie** IDOR/authz/overfetch).
- CVEs vía OSV.dev (RF-09).
- Consentimiento explícito para critical security.
- **MCP server funcional**: `codefit-scan-security`, `codefit-surface-*`, `codefit-scan-all`, `codefit-coverage`.
- `codefit init` con enriquecimiento del `AGENT.md` (con confirmación).
- `codefit-baseline` (RF-08).

**Done cuando:** un agente (OpenCode/Claude Code) llama `codefit-scan-security` durante una sesión, recibe findings + superficie, razona la superficie con su LLM, y el bloqueo conductual funciona vía AGENT.md. Detecta ≥3 categorías de vulnerabilidades con cero falsos positivos en secretos. Corre sobre Go (self-audit) y TypeScript.

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
- Licencia Apache 2.0, tag v0.1.0.

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
| 11 | Transport MCP | ✅ stdio default, HTTP/SSE opcional |
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
| 24 | Bloqueo de commit | ✅ Conductual: codefit informa `blocked`, el AGENT.md define la conducta |
| 25 | AGENT.md | ✅ codefit lo enriquece solo con confirmación del dev |
| 26 | Autonomía | ✅ Principio transversal: el developer siempre decide; se informan consecuencias |
| 27 | Nombres de tools | ✅ `codefit-` + guión medio; familia `codefit-surface-*` para mapeo |

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
| Descripción | "CLI + MCP server that audits what AI-generated code hides: security risks, algorithmic complexity, DB quality & code review." |

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

| Milestone | Contenido |
|---|---|
| `v0.1.0` | Fases 0+1 (Núcleo + Go + TS + Security + MCP) — primera release pública |
| `v0.2.0` | Fase 2 (DB) |
| `v0.3.0` | Fase 3 (Review + Practices + Tests) |
| `v1.0.0` | Fase 4 (Conocimiento + estabilidad de interfaz MCP) — release estable |
| `v1.1.0` | Java provider |
| `v1.2.0` | Python provider |

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
| **SurfaceItem** | Un elemento de superficie (categoría, ubicación, snippet, razón a revisar) entregado al agente. |
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
