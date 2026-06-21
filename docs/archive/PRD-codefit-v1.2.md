# PRD — codefit
**Product Requirements Document v1.2**
**Estado:** Final · **Autor:** Lucas (Architect) · **Fecha:** Junio 2026

---

## Tabla de contenidos

1. [Resumen ejecutivo](#1-resumen-ejecutivo)
2. [Problema](#2-problema)
3. [Filosofía del producto](#3-filosofía-del-producto)
4. [Objetivos y no-objetivos](#4-objetivos-y-no-objetivos)
5. [Usuarios target](#5-usuarios-target)
6. [Visión del producto](#6-visión-del-producto)
7. [Modos de operación: CLI y MCP](#7-modos-de-operación-cli-y-mcp)
8. [Requerimientos funcionales](#8-requerimientos-funcionales)
9. [CLI — Diseño e interfaz](#9-cli--diseño-e-interfaz)
10. [MCP Server — Diseño e interfaz](#10-mcp-server--diseño-e-interfaz)
11. [Autenticación y configuración de LLM](#11-autenticación-y-configuración-de-llm)
12. [Archivo de configuración de proyecto](#12-archivo-de-configuración-de-proyecto)
13. [Arquitectura técnica](#13-arquitectura-técnica)
14. [Arquitectura de extensibilidad: núcleo y language providers](#14-arquitectura-de-extensibilidad-núcleo-y-language-providers)
15. [Optimización de rendimiento y tokens](#15-optimización-de-rendimiento-y-tokens)
16. [Sensores — Especificación detallada](#16-sensores--especificación-detallada)
17. [Sandbox de ejecución](#17-sandbox-de-ejecución)
18. [Sistema de reporte](#18-sistema-de-reporte)
19. [Modos de integración](#19-modos-de-integración)
20. [Soporte de plataformas y lenguajes](#20-soporte-de-plataformas-y-lenguajes)
21. [Rollout por fases](#21-rollout-por-fases)
22. [Roadmap futuro / Ideas](#22-roadmap-futuro--ideas)
23. [Métricas de éxito](#23-métricas-de-éxito)
24. [Decisiones resueltas](#24-decisiones-resueltas)
25. [GitHub y open source](#25-github-y-open-source)
26. [Glosario](#26-glosario)

---

## 1. Resumen ejecutivo

`codefit` es una herramienta open source, escrita en Go, que audita proyectos de software generados (parcial o totalmente) con IA. Su premisa central es detectar todo aquello que el desarrollador **nunca va a ver** durante el desarrollo normal: vulnerabilidades de seguridad, complejidad algorítmica que escala mal, problemas estructurales de base de datos, riesgo de regresión en tests, y problemas de calidad de código que solo aparecen con revisión profunda.

Opera en **dos modos** sobre el mismo núcleo de sensores:
- **Modo CLI:** ejecución manual o en CI/CD desde la terminal. El desarrollador (o un pipeline) corre `codefit scan` y lee el reporte.
- **Modo MCP:** `codefit` se expone como servidor MCP (Model Context Protocol). Los agentes de IA (Claude Code, OpenCode, Cursor, etc.) llaman a los sensores como herramientas *durante* la generación de código, permitiendo auto-corrección dentro del ciclo de desarrollo.

La arquitectura separa un **núcleo universal** (orquestación, optimización, reporting, transporte) de **language providers** intercambiables, de modo que incorporar un lenguaje nuevo no requiere modificar el núcleo. Esto permite escalar de TypeScript a Java, Python y más sin fricción ni degradación.

No reemplaza TDD, SDD, linters ni herramientas de infraestructura. Es la capa de auditoría independiente que valida que el código generado sea seguro, correcto y escalable antes de mergear a producción.

---

## 2. Problema

### Contexto

El desarrollo con IA (vibe coding, SDD, agentes autónomos, Claude Code, OpenCode) democratizó la escritura de código. Una descripción en un prompt produce una aplicación funcional. Esto es poderoso, pero desplaza responsabilidades que antes cubría la experiencia del desarrollador:

- El agente genera código que **pasa los tests** y **cumple los criterios de aceptación visibles**.
- Nadie verifica lo que no se ve: vectores de ataque, curvas de crecimiento, índices faltantes, procedimientos inseguros, tests que no cubren los caminos críticos, acoplamiento que rompe funcionalidades al agregar código nuevo.

### Lo que el desarrollador no ve (y codefit sí)

| Dimensión | Por qué es invisible | Consecuencia si no se detecta |
|---|---|---|
| **Seguridad** | El código funciona; el ataque no ocurre en desarrollo | Vulnerabilidad en producción: breach, pérdida de datos, RCE |
| **Complejidad algorítmica** | Con datos de test (pocos registros), todo es rápido | Colapso de performance en producción con carga real |
| **Calidad de DB** | Las queries funcionan en desarrollo | Degradación exponencial a escala, inconsistencias de datos |
| **Code review profundo** | El desarrollador tiene blind spots sobre su propio código | Deuda técnica acumulada, imposibilidad de escalar el equipo |
| **Riesgo de regresión** | La nueva feature funciona; lo que rompió no tiene test | Bug silencioso en producción descubierto por un usuario |

### Por qué las herramientas existentes no alcanzan

- **TDD / Jest / JUnit / pytest**: excelentes para verificar comportamiento funcional. No detectan vectores de seguridad, no miden complejidad empírica, no analizan la calidad estructural de la DB.
- **SDD (Specification-Driven Development)**: excelente para garantizar que el código implementa los requerimientos. No audita la calidad interna de la implementación.
- **Linters / SAST básicos (ESLint, Checkstyle)**: analizan sintaxis y patrones conocidos. No hacen análisis dinámico ni inferencia semántica.
- **Code review con LLM (Skywork Skills, CodeRabbit)**: revisan el diff de texto en el PR. No ejecutan el código, no miden curvas de crecimiento, no tienen contexto del sistema completo.
- **Scanners de seguridad (Snyk, OWASP ZAP)**: especializados en dependencias o en runtime. No combinan análisis estático con contexto del código generado por IA.

`codefit` cierra la brecha entre "funciona" y "puede ir a producción".

---

## 3. Filosofía del producto

### Principio rector

> **codefit audita lo que el desarrollador no va a ver nunca.**

Este principio define qué está dentro y qué está fuera del scope en cualquier decisión de diseño. Si una dimensión es visible durante el desarrollo normal (la feature funciona, el test pasa, la UI renderiza), no es responsabilidad de `codefit`. Si es invisible (el vector de ataque, la curva de crecimiento O(n²), el índice faltante que no se nota con 100 registros), es exactamente donde `codefit` agrega valor.

### Corolario: consentimiento explícito sobre riesgos

Cuando `codefit` detecta un riesgo de seguridad crítico o un problema que puede comprometer la integridad del sistema, **no silencia el hallazgo** aunque el desarrollador lo ignore en la configuración. Los hallazgos de severidad `critical` en la dimensión de seguridad solo pueden suprimirse con una declaración explícita de consentimiento en `.codefit.yaml`, que queda commiteada al repositorio como registro de la decisión.

```yaml
# Ejemplo de supresión con consentimiento explícito
ignore:
  findings:
    - id: "SEC-012"
      reason: "Uso intencional de eval() para motor de scripts — sandboxeado externamente"
      accepted_by: "lucas@gentlemanprogramming.com"
      accepted_at: "2026-06-18"
```

### No invasividad

El código auditado no sabe que `codefit` existe. Sin decoradores, sin imports, sin modificaciones al código fuente del proyecto.

---

## 4. Objetivos y no-objetivos

### Objetivos

- Detectar vulnerabilidades de seguridad en el código antes del deploy o del merge.
- Medir complejidad algorítmica empírica de funciones críticas.
- Auditar la calidad estructural de la base de datos: normalización, índices, vistas, procedimientos, y soporte explícito para esquemas OLAP.
- Realizar code review profundo con LLM sobre el código del proyecto.
- Auditar la calidad y cobertura de la suite de tests, e identificar riesgo de regresión.
- Generar un reporte accionable, multi-dimensional, con score y severidad.
- Integrarse sin fricción en proyectos nuevos y proyectos en curso.
- Soportar Linux y Windows desde el primer release.
- Ser completamente open source.

### No-objetivos (explícitos)

- **No verifica requerimientos funcionales.** Para eso existe SDD. `codefit` no sabe ni le importa si la feature implementa el PRD.
- **No ejecuta tests.** Para eso existe TDD. `codefit` audita la calidad y cobertura de la suite, pero nunca ejecuta `jest`, `pytest` ni `mvn test`.
- **No genera código.** No sugiere refactors automáticos (capa futura posible, no MVP).
- **No monitorea producción.** No se integra con APMs ni analiza tráfico real.
- **No audita contratos entre microservicios ni APIs externas.**
- **No reemplaza un pentest.** Detecta patrones de vulnerabilidades conocidas; no hace explotación activa ni fuzzing.

---

## 5. Usuarios target

### Primario: desarrollador que usa IA como implementador

Escribe prompts, recibe implementaciones, las integra. Tiene contexto del dominio pero puede no tener experiencia profunda en seguridad, algoritmia o diseño de DB. Necesita validación automática antes de mergear.

### Secundario: arquitecto que delega implementación a agentes

Diseña el sistema, delega la implementación a Claude Code, OpenCode, Cursor, etc. Necesita una capa de auditoría que actúe como QA automatizado después de cada ciclo de generación.

### Terciario: equipo con CI/CD

Quiere un gate de calidad antes de mergear a main que detecte seguridad, calidad y regresión más allá de los tests.

---

## 6. Visión del producto

### Propuesta de valor

> "codefit te dice si el código que generaste con IA puede ir a producción — no solo si funciona, sino si es seguro, escalable y no va a romper lo que ya existe."

### Posicionamiento en el ecosistema

```
SDD          → garantiza que el código implementa los requerimientos
TDD          → garantiza que el código funciona según lo especificado
codefit      → garantiza que el código es seguro, escalable y de calidad

IA genera código
     ↓
[SDD verifica que implementa los reqs]
     ↓
[TDD verifica que funciona]
     ↓
[codefit audita lo que no se ve]
     ↓
Merge a main / Deploy a producción
```

`codefit` no compite con ninguna de las otras capas. Las complementa. Las tres son necesarias; ninguna reemplaza a las otras.

---

## 7. Modos de operación: CLI y MCP

`codefit` tiene dos modos de invocación que comparten exactamente el mismo núcleo de sensores. La lógica de auditoría es idéntica; solo cambia la capa de transporte y quién decide cuándo ejecutar.

### Modo CLI (reactivo)

El desarrollador o un pipeline de CI/CD ejecuta `codefit` desde la terminal. Es reactivo: corre cuando alguien lo invoca explícitamente.

```
Developer termina un PR
        ↓
codefit scan --since origin/main
        ↓
codefit corre sensores → genera reporte → lo muestra
        ↓
Developer lee y decide
```

Ventajas: simple, predecible, sin dependencias de un agente. Ideal para CI/CD, pre-commit hooks, y auditorías puntuales.

### Modo MCP (proactivo, integrado al ciclo de generación)

`codefit mcp serve` levanta un servidor MCP. Un agente de IA (orquestador) lo consume como un conjunto de herramientas. El agente llama a los sensores *durante* la generación, no después.

```
Developer: "implementá autenticación con JWT"
        ↓
Orquestador (ej: Claude Opus en OpenCode) → genera código
        ↓
Orquestador llama mcp__codefit__scan_security("src/auth/")
        ↓
codefit corre el sensor → devuelve findings JSON al orquestador
        ↓
Orquestador evalúa: ¿hay críticos? → corrige antes de mostrar al developer
        ↓
[el bug nunca llega al developer]
```

### Relación de roles en modo MCP

Es fundamental entender que **codefit NO es un subagente del orquestador**. Es un servidor de herramientas.

```
┌─────────────────────────────────────────────────────┐
│              ORQUESTADOR (ej: Claude Opus)           │
│  - Decide las fases / el flujo                      │
│  - Llama herramientas MCP                           │
│  - Razona qué hacer con los findings                │
│  - Decide si iterar o avanzar                       │
└──────────────────┬──────────────────────────────────┘
                   │ llama tools MCP (síncrono)
                   ▼
┌─────────────────────────────────────────────────────┐
│           CODEFIT MCP SERVER (proceso Go)            │
│  - Recibe la llamada                                │
│  - Corre el/los sensor(es) pedidos                  │
│  - Hace sus PROPIAS llamadas LLM si las necesita    │
│    (con su propio modelo, independiente del         │
│     orquestador)                                    │
│  - Devuelve JSON de findings                        │
│  - NO razona sobre qué hacer con ellos              │
└─────────────────────────────────────────────────────┘
```

Diferencia clave:
- Un **subagente** tiene su propio loop de razonamiento y toma decisiones.
- codefit como **MCP server** recibe una llamada, ejecuta, devuelve. La inteligencia de "qué hacer con los findings" vive en el orquestador.

### Independencia de modelos

codefit tiene su propio modelo LLM configurado, **independiente** del agente que lo invoca. Cuando Claude Opus (orquestador) llama a `mcp__codefit__review_code`, codefit internamente puede usar Claude Sonnet o Haiku según su propia configuración. Son dos contextos y dos facturas de API distintas. Esto permite, por ejemplo, que el orquestador use un modelo caro de razonamiento mientras codefit usa un modelo más barato para tareas mecánicas.

### Modelo stateless (decisión de diseño)

En modo MCP, codefit es **stateless**: cada tool call es independiente y no mantiene memoria de llamadas anteriores dentro de una sesión. El orquestador es responsable de acumular los findings de todas las llamadas y decidir.

Justificación:
- **Robustez:** no hay estado de sesión que pueda corromperse o quedar inconsistente.
- **Simplicidad:** cada llamada es una función pura (entrada → salida).
- **Escalabilidad:** múltiples agentes pueden usar el mismo servidor sin interferencia.

El único costo del stateless es la repetición del system prompt y contexto en cada llamada, que se mitiga casi por completo con **prompt caching** (ver sección 15, Optimización).

### Tabla comparativa de modos

| Dimensión | Modo CLI | Modo MCP |
|---|---|---|
| ¿Cuándo corre? | Cuando se invoca explícitamente | Cuando el agente lo decide, durante la generación |
| Quién lo dispara | Developer o pipeline | Agente orquestador |
| Fricción | Media (cambiar de contexto) | Cero (automático) |
| Cobertura | Variable (puede olvidarse) | Sistemática (si el perfil lo define) |
| Granularidad | Proyecto o diff completo | Archivo por archivo, en tiempo real |
| Estado | N/A | Stateless |
| Caso de uso ideal | CI/CD, releases, hooks | Loop de auto-corrección en SDD |

Ambos modos se mantienen en paridad funcional: cualquier sensor disponible en CLI lo está en MCP y viceversa.

---

## 8. Requerimientos funcionales

### RF-01 · Sensor de Seguridad *(dimensión más crítica)*

El sistema debe detectar vulnerabilidades de seguridad en el código fuente antes de que lleguen a producción. Este sensor tiene la mayor prioridad de todas las dimensiones.

**Categorías de detección:**

**Secretos y credenciales hardcodeadas**
- API keys, tokens, passwords, connection strings en código fuente o archivos de configuración commiteados.
- Detección por patrones (regex de alta precisión) + análisis semántico LLM.
- IDs de finding: SEC-001 a SEC-009.

**Vulnerabilidades de inyección**
- SQL Injection en queries dinámicas (concatenación de strings, interpolación sin parametrizar).
- Command Injection (uso de `exec`, `shell`, `subprocess` con input no sanitizado).
- XSS en React: uso de `dangerouslySetInnerHTML` con input no sanitizado.
- Template Injection en código de servidor.
- IDs de finding: SEC-010 a SEC-019.

**Autenticación y autorización**
- Endpoints sin verificación de autenticación.
- JWT: algoritmo `none`, secreto débil hardcodeado, sin verificación de expiración.
- Patrones IDOR (Insecure Direct Object Reference): acceso a recursos por ID sin verificar ownership.
- Ausencia de rate limiting en endpoints de autenticación.
- IDs de finding: SEC-020 a SEC-029.

**Exposición de datos sensibles**
- PII o datos sensibles en logs (`console.log`, `logger.info` con objetos que pueden contener passwords, emails, tokens).
- Campos sensibles en DB sin cifrado en reposo (detección heurística por nombre de columna: `password`, `ssn`, `credit_card`, `secret`).
- Respuestas de API que exponen más datos de los necesarios (over-fetching de campos sensibles).
- IDs de finding: SEC-030 a SEC-039.

**Configuración insegura**
- CORS demasiado permisivo (`origin: *` en producción).
- Headers de seguridad ausentes (CSP, HSTS, X-Frame-Options).
- Modo debug habilitado en configuración de producción.
- Dependencias con CVEs conocidos (análisis de `package.json`, `pom.xml`, `requirements.txt` contra base de CVEs pública).
- IDs de finding: SEC-040 a SEC-049.

**Criptografía**
- Algoritmos débiles o deprecados: MD5/SHA1 para hashing de passwords.
- Uso de `Math.random()` para tokens o valores de seguridad.
- Salts hardcodeados o ausentes en hashing de passwords.
- IDs de finding: SEC-050 a SEC-059.

**Política de consentimiento para hallazgos críticos de seguridad:**
Los hallazgos de severidad `critical` en esta dimensión NO pueden ignorarse con un simple `ignore.paths`. Requieren declaración explícita con `accepted_by`, `accepted_at` y `reason`. El reporte siempre los lista, aunque estén suprimidos, marcándolos como "aceptados con consentimiento".

---

### RF-02 · Sensor de Code Review

El sistema debe realizar un code review profundo del código del proyecto, similar al que haría un desarrollador senior experimentado. Este sensor cubre lo que los linters no pueden ver porque requiere comprensión del contexto y la intención.

**Qué revisa:**
- Legibilidad y claridad del código: nombres de variables, funciones y clases que no expresan la intención.
- Complejidad cognitiva excesiva: funciones con demasiadas responsabilidades, anidamiento profundo.
- Duplicación de lógica que debería estar abstraída.
- Manejo de errores: ausencia de catch, errores silenciados, mensajes de error que exponen detalles internos.
- Consistencia: código que no sigue los patrones establecidos en el resto del proyecto.
- Antipatrones específicos del lenguaje y framework (ver RF-05).
- Código muerto: funciones, variables, imports no utilizados.
- Comentarios desactualizados o contradictorios respecto al código.

**Modo de operación:**
- El LLM recibe el código con contexto del proyecto (lenguaje, framework, otros archivos relacionados).
- Genera hallazgos con: severidad, ubicación (archivo:línea), descripción del problema, sugerencia concreta.
- Puede operar en modo `--since` para revisar solo el código modificado (ideal para PR review).

**Diferenciación respecto a herramientas existentes:**
A diferencia de los skills de Skywork o CodeRabbit que analizan el diff del PR, `codefit review` puede analizar el código completo con contexto del proyecto. Esto permite detectar duplicación con código existente, inconsistencia con patrones del proyecto, y problemas que solo son visibles cuando se ve el código en contexto y no aislado en un diff.

---

### RF-03 · Sensor de Base de Datos

El sistema debe analizar la calidad estructural de las bases de datos del proyecto. Soporta dos paradigmas distintos: **OLTP** (transaccional, aplicaciones web) y **OLAP** (analítico, data warehouses, data marts).

#### RF-03a · OLTP (PostgreSQL, MySQL, SQLite)

**Normalización:**
- FK sin índice correspondiente (determinístico) — DB-001
- Columnas multivaluadas (CSV en un campo) — DB-002
- Grupos repetidos de columnas — DB-003
- Posible violación de 2FN (dependencia parcial de la PK) — DB-101 (LLM)
- Posible violación de 3FN (dependencia transitiva) — DB-102 (LLM)

**Índices:**
- FK sin índice de soporte — DB-001 (crítico)
- Columna frecuentemente filtrada en código sin índice (detectada por análisis del ORM/queries) — DB-010
- Índices duplicados o redundantes (mismo set de columnas en distinto orden) — DB-011
- Índices nunca usados (detectable en esquemas con historial de queries) — DB-012 (info)
- Ausencia de índice compuesto donde las queries filtran por múltiples columnas — DB-013
- Índice en columna de alta cardinalidad con baja selectividad — DB-014

**Vistas (Views):**
- Vista que expone columnas sensibles sin restricción de acceso — DB-020 (SEC overlap)
- Vista con lógica compleja que debería ser una función o procedimiento — DB-021
- Vista materializada sin estrategia de refresh definida — DB-022
- Vista que referencia tablas eliminadas o renombradas — DB-023 (crítico)

**Procedimientos almacenados y funciones:**
- SQL dinámico dentro de procedimiento sin parametrización (SQL injection) — DB-030 (crítico, referencia a SEC-010)
- Procedimiento sin manejo de excepciones/rollback — DB-031
- Función con side effects no documentados (modifica datos en una función de lectura) — DB-032
- Procedimiento con lógica de negocio compleja que debería estar en la capa de aplicación — DB-033

**Triggers:**
- Trigger que modifica datos en cascada sin documentación — DB-040
- Trigger que llama a procedimientos externos (riesgo de dependencias ocultas) — DB-041

**General:**
- Tabla sin PRIMARY KEY — DB-050 (crítico)
- Columna con tipo TEXT usada como FK — DB-051
- Ausencia de timestamps de auditoría en tablas de entidad — DB-052
- Campos sensibles sin mecanismo de cifrado — DB-053

#### RF-03b · OLAP / Data Warehouse / Data Mart

El sistema debe detectar cuando el schema sigue un paradigma analítico (estrella, copo de nieve, vault) y aplicar reglas distintas a las de 3FN.

**Detección automática del paradigma:**
- `codefit` detecta el paradigma OLAP por: presencia de tablas con prefijo/sufijo `fact_`, `dim_`, `stg_`, `mart_`; ausencia de 3FN intencional en tablas de dimensiones; columnas de fecha de tipo surrogate key.
- El usuario puede declararlo explícitamente en `.codefit.yaml`: `database.paradigm: olap`.

**Esquema estrella y copo de nieve:**
- Tabla de hechos sin FK a todas las dimensiones del grain declarado — DW-001
- Dimensión sin surrogate key (usando natural key como PK) — DW-002
- Grain de la tabla de hechos no documentado — DW-003 (info)
- Dimensión con columnas calculadas que deberían estar en la tabla de hechos — DW-004
- Ausencia de dimensión de tiempo (date dimension) — DW-005

**Slowly Changing Dimensions (SCDs):**
- Dimensión que parece SCD Type 2 (tiene `valid_from`, `valid_to`) sin índice en `is_current` o `valid_to` — DW-010
- Mezcla de estrategias SCD en la misma dimensión — DW-011
- SCD Type 2 sin columna `is_current` o equivalente — DW-012

**Performance OLAP:**
- Tabla de hechos grande sin particionamiento por fecha — DW-020
- Ausencia de índices columnares en bases que los soportan (PostgreSQL con extensiones, SQL Server) — DW-021
- Vista materializada para agregaciones sin estrategia de refresh — DW-022

**Nota:** En paradigmas OLAP, la desnormalización intencional NO se reporta como violación de 3FN. El sensor detecta automáticamente el contexto y aplica las reglas correspondientes.

---

### RF-04 · Sensor de Complejidad Algorítmica

Para funciones marcadas en la configuración, el sistema ejecuta benchmarks en un sandbox Docker y clasifica la complejidad empírica.

**Flujo:**
1. Compilar/preparar harness en contenedor Docker efímero.
2. Ejecutar la función con entradas n = [10, 100, 1.000, 10.000, 100.000] (configurable).
3. Medir tiempo promedio por n (múltiples runs para reducir varianza).
4. Ajustar por regresión a curvas canónicas: O(1), O(log n), O(n), O(n log n), O(n²), O(n³), O(2ⁿ).
5. Reportar best fit con R² (nivel de confianza).
6. Marcar como crítico si el fit sugiere O(n²) o peor y supera el umbral configurado.

**Input del usuario:** función generadora de datos de prueba (ver sección de configuración).

**Sin acceso a DB real:** las funciones que requieren DB deben recibir los datos como parámetro. El sandbox no tiene acceso a red ni a bases de datos externas. El generador de inputs es responsabilidad del desarrollador.

---

### RF-05 · Sensor de Best Practices

Detecta violaciones de las mejores prácticas del lenguaje y framework que son especialmente frecuentes en código generado por IA.

**React / TypeScript (v1.0):**
- Uso de `any` como tipo (configurable: error | warn)
- Props no tipadas en componentes
- Dependencias incorrectas o ausentes en hooks (`useEffect`, `useMemo`, `useCallback`)
- `useEffect` con lógica de negocio compleja
- Ausencia de manejo de errores en llamadas async/await
- `console.log` en código (no en archivos de test)
- Variables de entorno accedidas sin validación de tipo
- Componentes con más de N props sin abstraer (umbral configurable)
- `dangerouslySetInnerHTML` sin sanitización (overlap con SEC-011)

**Java (v2.0):**
- Ausencia de manejo de excepciones checked
- Uso de tipos raw en generics
- Recursos no cerrados (streams, connections) fuera de try-with-resources
- Ausencia de `@Override` en implementaciones de interfaces

**Python (v3.0):**
- Mutables como argumentos por defecto de funciones
- Imports circulares
- Ausencia de type hints en funciones públicas
- `except Exception` sin re-raise o logging

---

### RF-06 · Sensor de Tests y Riesgo de Regresión

El sistema **no ejecuta tests**. Audita la calidad de la suite existente y calcula el riesgo de regresión que introduce el código nuevo o modificado.

**Auditoría de la suite de tests:**
- Detección de ausencia total de tests en el proyecto — TEST-001 (crítico)
- Funciones marcadas como críticas en `.codefit.yaml` sin test correspondiente detectado — TEST-002
- Tests sin assertions (test que pasa siempre) — TEST-003
- Tests que no cubren el camino del error (solo happy path) — TEST-004 (LLM)
- Tests duplicados (misma lógica testeada múltiples veces sin variación) — TEST-005

**Riesgo de regresión (modo `--since`):**
Cuando se ejecuta en modo incremental, el sensor analiza el código modificado y evalúa:
- ¿El código modificado está cubierto por tests existentes? Si no: riesgo de regresión alto.
- ¿Las firmas o contratos de funciones públicas cambiaron? Si sí: listar todos los callsites afectados.
- ¿Se modificaron funciones compartidas (utilities, helpers) sin tests? Riesgo de regresión en múltiples features.
- ¿Se modificó la capa de DB (schema, migrations)? Listar todos los modelos y queries potencialmente afectados.

Este análisis de riesgo de regresión es el valor diferencial del sensor: **no dice si algo está roto, dice qué puede estar roto** como consecuencia de los cambios recientes. La decisión de correr los tests (y cuáles) queda en manos del desarrollador.

---

### RF-07 · Generación de Reporte Multi-dimensional

El sistema genera un reporte con score global (0–100), score por dimensión, lista completa de hallazgos con severidad y recomendación, y resumen ejecutivo opcional via LLM.

**Dimensiones y pesos por defecto (configurables):**

| Dimensión | Peso default | Justificación |
|---|---|---|
| Seguridad | 35% | Un solo hallazgo crítico puede invalidar el deploy |
| Code Review | 20% | Calidad de largo plazo, mantenibilidad |
| Base de Datos | 20% | Problemas estructurales costosos de corregir a posteriori |
| Complejidad Algorítmica | 15% | Impacto en escala |
| Tests / Regresión | 10% | Asume que TDD está en uso; este sensor es complementario |

**Formatos de salida:** JSON (canónico, siempre), Markdown (terminal y PRs), HTML standalone (reportes compartibles).

---

### RF-08 · Modo Incremental (diff-based)

Ejecuta solo los sensores estáticos sobre archivos modificados desde un commit de referencia. Esencial para integración en flujos de trabajo diarios sin auditar el proyecto completo cada vez.

---

### RF-09 · Inicialización

`codefit init` analiza el proyecto y genera `.codefit.yaml` inicial detectando automáticamente: lenguaje, framework, ORM, paradigma de DB (OLTP vs OLAP), archivos de test, y estructura de directorios.

---

### RF-10 · Baseline (adopción sin fricción)

Cuando un proyecto existente corre codefit por primera vez, puede aparecer una gran cantidad de findings (deuda histórica) que abruma al usuario y lleva al abandono. El comando `codefit baseline` resuelve esto:

- Toma una foto del estado actual de findings y la guarda en `.codefit/baseline.json` (se commitea).
- A partir de ahí, con `baseline.enabled: true`, codefit solo reporta findings **nuevos** — los introducidos por código posterior al baseline.
- La deuda histórica queda registrada (marcada `baselined: true`) pero no genera ruido ni bloquea.
- `codefit baseline --update` regenera la foto (por ejemplo, tras saldar deuda).

Esto hace que adoptar codefit en un proyecto en curso sea indoloro: el desarrollador ve solo lo que su código nuevo introduce, no la herencia. Es la diferencia entre adopción y abandono en el primer scan.

---

### RF-11 · Criticidad por contexto de path

No todos los findings de la misma regla tienen la misma gravedad según dónde estén. Un secreto hardcodeado en un archivo de test es ruido; en `production.config.ts` es crítico. Un `console.log` en un ejemplo es informativo; en un endpoint de pagos es un problema.

codefit pondera la severidad de cada finding según la clasificación de path declarada en `project.path_criticality` (production / test / example). El `LanguageProvider` puede aportar defaults sensatos por ecosistema (qué directorios son típicamente tests, ejemplos, etc.). Esto reduce falsos positivos molestos y sube la relación señal/ruido, que es crítica para que el usuario confíe en la herramienta.

---

## 9. CLI — Diseño e interfaz

### Subcomandos principales

```
codefit <subcommand> [flags] [path]
```

| Subcomando | Descripción |
|---|---|
| `init` | Wizard interactivo. Analiza el proyecto y genera `.codefit.yaml`. |
| `scan` | Ejecuta todos los sensores estáticos. No requiere Docker. |
| `bench` | Ejecuta los benchmarks de complejidad en sandbox Docker. |
| `review` | Ejecuta solo el sensor de code review (LLM). |
| `report` | Renderiza el último resultado JSON en el formato indicado. |
| `run` | Ejecuta `scan` + `bench` secuencialmente. |
| `baseline` | Toma una foto del estado actual. Findings existentes pasan a deuda histórica; solo se reportan los nuevos. |
| `mcp serve` | Levanta el servidor MCP para integración con agentes de IA. |
| `auth` | Gestiona la autenticación con providers LLM. |
| `set` | Configura parámetros globales (modelo, proveedor). |
| `status` | Muestra configuración activa: provider, modelo, versión. |

### Flags globales

```
--config <path>        Ruta alternativa al .codefit.yaml (default: ./.codefit.yaml)
--output <formato>     json | markdown | html (default: markdown)
--out-file <path>      Escribe el output a un archivo
--fail-on <severidad>  critical | high | medium. Exit code 1 si hay hallazgos de este nivel.
                       (default: critical)
--quiet                Solo muestra score final y hallazgos críticos
--no-llm               Desactiva análisis que requieren LLM (code review, 3FN, resumen)
--verbose              Log detallado de cada paso
```

### Flags de `scan`

```
--since <ref>          Solo archivos modificados desde el commit/branch dado.
                       Ejemplos: HEAD~1, origin/main, abc1234
--sensor <nombre>      Ejecuta solo el sensor indicado. Repetible.
                       Valores: security | review | db | complexity | practices | tests
```

### Flags de `review`

```
--since <ref>          Revisa solo código modificado desde la referencia dada
--context <n>          Líneas de contexto a incluir alrededor del código modificado (default: 50)
```

### Flags de `bench`

```
--function <id>        Ejecuta solo el benchmark identificado en el config
--dry-run              Construye el harness pero no ejecuta
--n-values <lista>     Override: --n-values 10,100,1000,10000
```

### Ejemplos de uso completo

```bash
# Primera vez en un proyecto
codefit auth login
codefit init
codefit run

# Auditoría completa con reporte HTML
codefit run --output html --out-file ./reports/audit-$(date +%Y%m%d).html

# Solo lo modificado desde main (modo diario)
codefit scan --since origin/main

# Code review del PR antes de mergear
codefit review --since origin/main --output markdown

# Solo el sensor de seguridad (más crítico)
codefit scan --sensor security

# CI/CD: falla si hay críticos o altos
codefit run --fail-on high --quiet

# Configurar modelo local
codefit set model qwen3 --local --url http://localhost:11434

# Ver configuración activa
codefit status
```

### Salida en terminal (modo markdown)

```
codefit v0.1.0 · React/TypeScript + PostgreSQL (OLTP)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  SCORE GLOBAL          64 / 100
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  🔒 Seguridad          41 / 100  ●●○○○  ← BLOQUEANTE
  🔍 Code Review        72 / 100  ●●●●○
  📊 Base de Datos      58 / 100  ●●●○○
  ⚡ Complejidad         81 / 100  ●●●●○
  🧪 Tests / Regresión  71 / 100  ●●●●○
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

⛔ CRÍTICOS - SEGURIDAD (2)  [BLOQUEAN DEPLOY]
  [SEC-001] API key de Anthropic hardcodeada en código fuente
            src/lib/ai.ts:12
            → Mover a variable de entorno: process.env.ANTHROPIC_API_KEY

  [SEC-021] Endpoint /api/plants sin verificación de autenticación
            src/routes/plants.ts:34
            → Agregar middleware de auth antes del handler

⛔ CRÍTICOS - DB (1)
  [DB-001] FK sin índice: plants.user_id → users.id
            prisma/schema.prisma:34
            → Agregar @@index([user_id]) en modelo Plant

⚠️  ALTOS (3)   [ver reporte completo]
⚡ MEDIOS (5)   [ver reporte completo]

Riesgo de regresión detectado:
  → src/services/auth.ts modificado: 8 callsites sin cobertura de test

Reporte completo: ./codefit-report.json
```

---

## 10. MCP Server — Diseño e interfaz

### Activación

```bash
codefit mcp serve              # Levanta el servidor MCP (stdio transport por defecto)
codefit mcp serve --port 7777  # Opcional: transport HTTP/SSE para clientes remotos
```

### Configuración en el cliente agente

**Claude Code / Claude Desktop:**
```json
{
  "mcpServers": {
    "codefit": {
      "command": "codefit",
      "args": ["mcp", "serve"],
      "cwd": "/path/to/project"
    }
  }
}
```

**OpenCode:**
```json
{
  "mcp": {
    "codefit": {
      "type": "local",
      "command": ["codefit", "mcp", "serve"],
      "enabled": true
    }
  }
}
```

### Herramientas expuestas

Cada herramienta MCP mapea a uno o más sensores del núcleo. Son stateless: reciben todo lo necesario como parámetros y devuelven findings en JSON.

| Tool MCP | Sensor(es) | Parámetros | Devuelve |
|---|---|---|---|
| `scan_security` | Seguridad | `path`, `since?` | Findings de seguridad |
| `review_code` | Code Review | `path`, `context_lines?` | Findings de review |
| `scan_db` | Base de Datos | `schema_path` | Findings de DB |
| `check_practices` | Best Practices | `path`, `language?` | Findings de best practices |
| `scan_tests` | Tests / Regresión | `path`, `since?` | Findings + riesgo de regresión |
| `scan_all` | Todos los estáticos | `path`, `since?` | Findings agregados |
| `bench_function` | Complejidad | `function_id` | Clasificación de complejidad + R² |
| `describe_rules` | — | `sensor?` | Catálogo de reglas y sus IDs (para que el agente entienda qué detecta cada sensor) |

### Contrato de respuesta de las tools

Toda tool devuelve el mismo shape JSON (subset del reporte canónico):

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
  "summary": {
    "critical": 1, "high": 0, "medium": 0, "low": 0, "info": 0
  },
  "blocked": true
}
```

El campo `blocked: true` le indica al orquestador que hay hallazgos críticos de seguridad que deberían detener el avance, aunque la decisión final es del orquestador.

### Patrones de uso por el orquestador

**Patrón 1 — Auditoría tras generación (el más común):**
```
Agente genera código → llama scan_security(path) → evalúa findings → corrige o avanza
```

**Patrón 2 — Loop de auto-corrección:**
```
1. scan_security → encuentra crítico
2. orquestador NO llama otros sensores (ahorra tokens)
3. orquestador regenera el código con el finding como contexto
4. scan_security de nuevo → sin críticos
5. ahora sí: review_code, scan_db, etc.
6. avanza
```

**Patrón 3 — Auditoría completa al cerrar feature:**
```
scan_all(path, since) → reporte completo → resumen al developer
```

### Integración con el ciclo SDD (OpenCode)

codefit se engancha como una fase explícita del ciclo SDD. El perfil `sdd-audit` de OpenCode tiene un system prompt como:

```
Sos el auditor del ciclo SDD. Tras la fase de implementación,
usá las herramientas de codefit MCP para auditar lo generado.
Empezá siempre por scan_security. Si hay hallazgos críticos o altos,
NO avancés a la siguiente fase: volvé a sdd-implement con el finding
como contexto. Solo cuando no haya críticos, corré review_code y scan_db,
y luego avanzá.
```

Esto crea un loop de auto-corrección dentro del pipeline de agentes, sin intervención humana hasta que el código pasa la auditoría de seguridad.

### Prompt caching en modo MCP

Como el modo es stateless, cada tool call repetiría el system prompt del sensor. Para evitar este costo, el MCP server usa **prompt caching de Anthropic** (y equivalentes en otros providers): el system prompt y el contexto estable del proyecto se marcan como cacheables. El overhead del stateless se reduce a una fracción mínima. Esto es un requisito de implementación desde la Fase 0 del MCP server (ver sección 15).

### Paridad y reutilización

El MCP server NO reimplementa lógica. Es un adapter delgado que traduce llamadas MCP a invocaciones del mismo núcleo de sensores que usa el CLI. Cualquier mejora en un sensor beneficia ambos modos automáticamente.

---

## 11. Autenticación y configuración de LLM

### Filosofía

La configuración del LLM debe ser tan intuitiva como `gh auth login`. Sin editar archivos de configuración manualmente, sin buscar dónde pegar una API key. El flujo estándar son dos comandos:

```bash
codefit auth login     # configura el proveedor
codefit set model X    # elige el modelo
```

### Proveedores soportados

| Provider | Tipo | Auth |
|---|---|---|
| Anthropic (Claude) | Cloud | API key |
| OpenAI (GPT-4o, o3) | Cloud | API key |
| Google (Gemini) | Cloud | API key |
| Groq | Cloud | API key |
| OpenRouter | Cloud (meta-provider) | API key |
| Ollama | Local | Sin auth, URL |
| LM Studio | Local | Sin auth, URL compatible OpenAI |

### Flujo `codefit auth login`

```
$ codefit auth login

? Seleccioná tu proveedor de LLM:
  ❯ Anthropic (Claude)
    OpenAI
    Google (Gemini)
    Groq
    OpenRouter
    Ollama (local)
    LM Studio (local)
    Otro (compatible OpenAI)

[selección: Anthropic]

→ Abriendo tu navegador en: https://console.anthropic.com/settings/keys
  (Si no se abre automáticamente, ingresá esa URL)

? Pegá tu API key aquí: sk-ant-...

✓ API key válida
✓ Credencial guardada en keychain del sistema

Modelo por defecto: claude-sonnet-4-6
Para cambiarlo: codefit set model <nombre>
```

**Almacenamiento seguro de credenciales:**

Las API keys se guardan en el keychain del sistema operativo:
- **Linux:** libsecret (GNOME Keyring / KWallet)
- **Windows:** Windows Credential Manager
- **Fallback:** archivo cifrado en `~/.config/codefit/credentials` con permisos 600

Las API keys **nunca** se escriben en `.codefit.yaml` ni en ningún archivo que pueda commitearse accidentalmente. Si el usuario quiere usar variable de entorno (para CI/CD), se respetan: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, etc.

### Flujo `codefit auth login` para modelos locales (Ollama)

```
$ codefit auth login

[selección: Ollama (local)]

? URL del servidor Ollama: [http://localhost:11434]

→ Probando conexión... ✓ Ollama v0.6.1 detectado

Modelos disponibles en tu instancia:
  • qwen3:30b
  • llama3.3:70b
  • deepseek-coder-v2:16b

? Seleccioná el modelo por defecto: ❯ qwen3:30b

✓ Configuración guardada
```

### Comandos de gestión

```bash
codefit auth login                    # Wizard interactivo de auth
codefit auth login --provider anthropic   # Ir directo a un provider
codefit auth logout                   # Elimina credenciales del keychain
codefit auth status                   # Muestra provider y modelo activos

codefit set model claude-opus-4-6          # Cambia el modelo (mismo provider)
codefit set model qwen3:30b               # En Ollama
codefit set model gpt-4o --provider openai  # Cambia provider y modelo

codefit status                        # Muestra: provider, modelo, versión codefit, Docker disponible
```

### Configuración global vs. por proyecto

La configuración de auth es **global** (usuario): `~/.config/codefit/config.yaml`. No se commitea.

El proyecto puede declarar en `.codefit.yaml` qué modelo prefiere para ese proyecto:

```yaml
llm:
  model: "claude-sonnet-4-6"      # Opcional. Si no se declara, usa el global.
  provider: "anthropic"           # Opcional.
```

Si hay conflicto, el proyecto tiene precedencia sobre el global. Las credenciales siempre vienen del keychain/env var, nunca del `.codefit.yaml`.

---

## 12. Archivo de configuración de proyecto

```yaml
# .codefit.yaml — se commitea al repositorio
version: "1"

project:
  name: "plantalinda-api"
  language: "typescript"          # typescript | java | python | go
  framework: "react"              # react | next | express | spring | fastapi | django
  description: "Cannabis cultivation management platform"
  # Criticidad por path: pondera la severidad según dónde está el finding.
  # Un secreto en un test es ruido; en producción es crítico.
  path_criticality:
    production:                   # Severidad se mantiene o sube
      - "src/**"
      - "*.config.ts"
    test:                         # Severidad se baja (menos ruido)
      - "**/*.test.ts"
      - "**/*.spec.ts"
    example:                      # Findings informativos, no bloquean
      - "examples/**"
      - "docs/**"

database:
  paradigm: "oltp"                # oltp | olap | mixed (default: auto-detectado)
  type: "postgresql"              # postgresql | mysql | sqlite | none
  schema_paths:
    - "prisma/schema.prisma"
    - "src/db/migrations/*.sql"
  orm: "prisma"                   # prisma | typeorm | drizzle | sequelize | hibernate | sqlalchemy | none

sensors:
  security:
    enabled: true
    scan_dependencies: true       # Verifica CVEs en package.json/pom.xml/requirements.txt
  review:
    enabled: true
    context_lines: 50             # Líneas de contexto para el LLM
  db:
    enabled: true
    llm_inference: true
    confidence_threshold: 0.75
    check_indexes: true           # Detecta índices faltantes, duplicados, redundantes
    check_views: true
    check_procedures: true
    check_triggers: true
  complexity:
    enabled: true
    fail_threshold: "n2"          # Crítico si el fit es O(n²) o peor
    min_confidence: 0.85          # R² mínimo para reportar
  practices:
    enabled: true
    rules:
      no_any: "error"
      missing_error_handling: "warn"
      console_log_in_prod: "warn"
      complex_useeffect: "warn"
      untyped_env_vars: "error"
  tests:
    enabled: true
    test_dirs:
      - "src/**/*.test.ts"
      - "src/**/*.spec.ts"
    check_regression_risk: true   # Analiza riesgo de regresión en modo --since

benchmarks:
  defaults:
    n_values: [10, 100, 1000, 10000, 100000]
    runs_per_n: 5
    timeout_seconds: 30
  functions:
    - id: "searchPlants"
      file: "src/services/search.ts"
      export: "searchPlants"
      generator: "benchmarks/generators/search.gen.ts"
      description: "Búsqueda de plantas por filtros combinados"

report:
  output: "markdown"
  out_file: "./codefit-report"
  include_info: false
  score_weights:                  # Deben sumar 100
    security: 35
    review: 20
    db: 20
    complexity: 15
    tests: 10

llm:
  model: "claude-sonnet-4-6"      # Modelo por defecto. Sobreescribe el global.
  prompt_caching: true            # Cachea system prompts y contexto estable (recomendado)
  batching: true                  # Agrupa archivos pequeños en una sola llamada
  # Routing de modelos por sensor (opcional). null = sin LLM (solo estático).
  sensor_models:
    security_semantic: "claude-sonnet-4-6"   # Razonamiento de seguridad: calidad alta
    review:            "claude-sonnet-4-6"    # Code review: necesita contexto profundo
    db_inference:      "claude-haiku-4-5"     # 3FN: tarea más simple, modelo barato
    regression_risk:   "claude-haiku-4-5"     # Riesgo de regresión: modelo barato
    summary:           "claude-haiku-4-5"     # Resumen ejecutivo: modelo barato

cache:
  enabled: true                   # Caché de findings por hash de contenido
  dir: ".codefit/cache"           # Agregar a .gitignore

baseline:
  enabled: false                  # Si true, solo reporta findings nuevos (no la deuda histórica)
  file: ".codefit/baseline.json"  # Foto del estado al correr 'codefit baseline'. Se commitea.

mcp:
  enabled: true                   # Permite 'codefit mcp serve'
  expose_tools:                   # Qué tools se exponen al agente (default: todas)
    - scan_security
    - review_code
    - scan_db
    - check_practices
    - scan_tests
    - scan_all
    - bench_function

ignore:
  paths:
    - "node_modules/**"
    - "dist/**"
    - "build/**"
    - "**/*.generated.ts"
  findings:
    # Hallazgos no-críticos: se ignoran silenciosamente
    - id: "DB-052"
      reason: "Sin timestamps por decisión de diseño en tablas de lookup"

    # Hallazgos críticos de seguridad: requieren consentimiento explícito
    - id: "SEC-042"
      reason: "CORS abierto intencional: API pública de lectura sin datos sensibles"
      accepted_by: "lucas@gentlemanprogramming.com"
      accepted_at: "2026-06-18"
```

---

## 13. Arquitectura técnica

### Stack

| Componente | Tecnología | Justificación |
|---|---|---|
| Núcleo / CLI / MCP | Go | Binario único sin runtime, cross-compile limpio, concurrencia nativa (goroutines), curva de contribución baja para open source, ecosistema de release (goreleaser) inmejorable |
| Parsing AST | tree-sitter **puro Go, sin CGO** | Decisión crítica: los bindings tradicionales de tree-sitter requieren CGO, lo que rompe el cross-compile y obliga a montar toolchains de C por plataforma. Se usa una implementación pura en Go (tipo go-treesitter) que preserva el binario único y es más rápida en parsing incremental (clave para `--since`) |
| MCP Server | Go (mismo binario) | `codefit mcp serve` expone los sensores como tools MCP sin proceso separado |
| Análisis semántico | LLM via API (cliente abstracto) | Code review, inferencia 3FN, resumen ejecutivo. Con prompt caching |
| Sandbox | Docker (contenedores efímeros) | Aislamiento para benchmarks, sin acceso a red/host |
| Harness | Código en el lenguaje target | El código auditado se ejecuta en su propio ecosistema |
| Keychain | go-keyring | Acceso al keychain del SO: Linux/Windows/macOS |
| Output | JSON → renderers (MD/HTML) | JSON como source of truth |

**¿Por qué Go y no Rust?** Rust ofrece ~2x más performance en tareas CPU-bound (parsing, regresión). Pero el cuello de botella de codefit no es CPU: el parsing con tree-sitter es sub-milisegundo y el tiempo real se va en latencia de llamadas LLM e I/O de disco. Optimizar el parsing sería optimizar el ~2% del tiempo total. A cambio, Go ofrece compilación más rápida, menor barrera de entrada para contribuidores (decisivo en open source), y el mejor tooling de distribución cross-platform que existe. Para este perfil de carga, Go es la elección correcta.

### Estructura del repositorio

```
codefit/
├── cmd/
│   └── codefit/
│       └── main.go
├── internal/
│   ├── cli/              # Subcomandos, flags, routing
│   ├── mcp/              # MCP server: adapter de tools MCP → núcleo
│   ├── config/           # Parser .codefit.yaml + config global ~/.config/codefit/
│   ├── auth/             # Keychain, env var fallback, provider clients
│   │
│   ├── core/             # ── NÚCLEO UNIVERSAL (language-agnostic) ──
│   │   ├── context/      # AuditContext — struct compartido entre sensores
│   │   ├── orchestrator/ # Orquestación y ejecución de sensores (paralelo, orden)
│   │   ├── pipeline/     # Pirámide de filtrado (capas 0-3)
│   │   ├── cache/        # Caché de findings por hash de contenido
│   │   ├── scoring/      # Cálculo de scores por dimensión y global
│   │   ├── llm/          # Cliente LLM abstracto + prompt caching + batching
│   │   ├── report/       # JSON canónico → Markdown / HTML
│   │   ├── findings/     # Tipos Finding, Severity, Dimension, ConsentRecord
│   │   └── complexity/   # Regresión de curvas (universal, no depende del lenguaje)
│   │
│   ├── sensors/          # ── SENSORES (orquestan capas; piden datos al provider) ──
│   │   ├── sensor.go     # Interface Sensor
│   │   ├── security/
│   │   ├── review/
│   │   ├── db/           # OLTP + OLAP
│   │   ├── complexity/   # Orquesta benchmarks; delega harness al provider
│   │   ├── practices/
│   │   └── tests/
│   │
│   ├── providers/        # ── LANGUAGE PROVIDERS (específico por lenguaje) ──
│   │   ├── provider.go   # Interface LanguageProvider
│   │   ├── golang/       # Provider de arranque: codefit se audita a sí mismo
│   │   ├── typescript/   # Queries tree-sitter, reglas, harness, mapeo ORM
│   │   ├── java/         # (v1.1)
│   │   └── python/       # (v1.2)
│   │
│   └── sandbox/          # Gestión de contenedores Docker
│
├── benchmarks/           # Templates de generadores para el usuario
├── docs/
└── README.md
```

La separación en tres capas — `core/` (universal), `sensors/` (lógica de auditoría) y `providers/` (específico por lenguaje) — es la decisión arquitectónica que permite escalar a nuevos lenguajes sin tocar el núcleo. Se detalla en la sección 14.

### Interfaz Sensor (contrato central)

```go
type Sensor interface {
    Name()     string
    Dimension() Dimension
    Run(ctx AuditContext) (SensorResult, error)
}

type Finding struct {
    ID            string      `json:"id"`
    Dimension     Dimension   `json:"dimension"`
    Severity      Severity    `json:"severity"`
    File          string      `json:"file,omitempty"`
    Line          int         `json:"line,omitempty"`
    Title         string      `json:"title"`
    Description   string      `json:"description"`
    Suggestion    string      `json:"suggestion"`
    Reasoning     string      `json:"reasoning,omitempty"`  // Por qué se marcó (findings LLM). Construye confianza.
    Confidence    float64     `json:"confidence"`       // 1.0 para determinísticos
    Probabilistic bool        `json:"probabilistic"`    // true si viene de LLM
    RequiresConsent bool      `json:"requires_consent"` // true para critical security
    Suppressed    *ConsentRecord `json:"suppressed,omitempty"`
    Baselined     bool        `json:"baselined,omitempty"`  // true si es deuda histórica registrada en baseline
}

type ConsentRecord struct {
    AcceptedBy string `json:"accepted_by"`
    AcceptedAt string `json:"accepted_at"`
    Reason     string `json:"reason"`
}
```

### Flujo de ejecución de `codefit run`

El flujo respeta la pirámide de filtrado (sección 15): los filtros baratos corren primero y el LLM solo procesa lo que las capas anteriores no resolvieron.

```
codefit run
│
├── Carga ~/.config/codefit/config.yaml (global: provider, modelo)
├── Carga .codefit.yaml (proyecto: sensores, config)
├── Verifica auth (keychain o env var)
├── Resuelve el LanguageProvider según project.language
├── Construye AuditContext
│
├── Capa 0 — Filtro de cambios (gratis)
│   └── Si --since: descarta archivos no modificados. Si hash en caché coincide: reutiliza findings.
│
├── Capa 1 — Patrones / regex (microsegundos, gratis)
│   └── Secretos hardcodeados, console.log, patrones obvios
│
├── Capa 2 — AST tree-sitter (sub-ms, gratis)
│   └── Estructura, N+1, missing await, tipos, best practices, candidatos sospechosos
│
├── [Si --fail-on se dispararía ya por capas 0-2 → corte temprano opcional, cero tokens LLM]
│
├── Capa 3 — LLM (segundos, $$$) — SOLO fragmentos marcados como sospechosos no concluyentes
│   ├── [Paralelo, con batching y prompt caching]
│   │   ├── Security semántico (IDOR, over-fetching)
│   │   ├── Code Review (chunks con contexto)
│   │   ├── DB inferencia 3FN
│   │   └── Tests: riesgo de regresión
│
├── [Secuencial, requiere Docker] Sensor Complejidad
│   └── Por cada función en config.benchmarks.functions:
│       ├── provider.BuildHarness() → imagen Docker
│       ├── Ejecutar n × runs → tiempos
│       ├── core/complexity: regresión de curvas (universal)
│       └── Finding con best fit + R²
│
├── Aplica supresiones del ignore block (con validación de consentimiento)
├── Guarda findings en caché por hash
├── Calcula scores por dimensión y score global
├── Serializa a JSON canónico
└── Renderiza al formato configurado
```

---

## 14. Arquitectura de extensibilidad: núcleo y language providers

Esta es la decisión arquitectónica que garantiza que codefit escale a cada lenguaje nuevo **sin fricción y sin degradación**. El riesgo que mitiga es real: si agregar Java significara reescribir sensores, el proyecto colapsaría bajo su propio peso al tercer lenguaje.

### Principio: separar lo universal de lo específico

```
┌─────────────────────────────────────────────────────┐
│         NÚCLEO UNIVERSAL (core/) — language-agnostic │
│  - Orquestación de sensores                          │
│  - Pirámide de filtrado (capas 0-3)                  │
│  - Caché por hash, scoring, reporting                │
│  - Cliente LLM, prompt caching, batching             │
│  - MCP server / CLI                                  │
│  - Regresión de curvas (complejidad)                 │
│  - Tipos Finding / Severity / Dimension              │
└──────────────────────┬───────────────────────────────┘
                      │ depende de la interface
                      ▼
┌─────────────────────────────────────────────────────┐
│      LANGUAGE PROVIDER (interface)                   │
│  Cada lenguaje implementa este contrato:             │
│                                                      │
│  - Queries tree-sitter para su gramática             │
│  - Reglas de best practices del ecosistema           │
│  - Harness de benchmarking propio                    │
│  - Mapeo de su ORM/schema al modelo común de DB      │
│  - Prompts especializados de code review             │
│  - Detección de tests del ecosistema                 │
└─────────────────────────────────────────────────────┘
```

### La interface LanguageProvider

```go
// internal/providers/provider.go

type LanguageProvider interface {
    // Identidad
    Language() string                    // "typescript", "java", "python"
    Frameworks() []string                // frameworks reconocidos
    FileExtensions() []string            // [".ts", ".tsx"]

    // Parsing — provee la gramática tree-sitter (pura Go, sin CGO)
    Grammar() *sitter.Language

    // Queries tree-sitter por categoría de detección
    SecurityQueries() []Query            // patrones de inyección, auth, etc.
    PracticeQueries() []Query            // best practices del lenguaje
    NPlusOneQueries() []Query            // detección de N+1 en su ORM

    // Best practices — reglas del ecosistema
    PracticeRules() []Rule

    // DB — mapeo del schema/ORM del lenguaje al modelo común
    ParseSchema(paths []string) (*DBSchema, error)

    // Complejidad — harness de benchmarking en el sandbox
    BuildHarness(fn BenchmarkTarget) (HarnessSpec, error)

    // Tests — detección de la suite del ecosistema
    DetectTests(ctx AuditContext) ([]TestFile, error)

    // Prompts — contexto especializado para el LLM
    ReviewPromptContext() string         // particularidades del lenguaje para el review
}
```

### Por qué esto funciona: tree-sitter unifica el parsing

La clave técnica que hace este diseño elegante es que **tree-sitter usa el mismo formato de queries para todos los lenguajes**. Una query para detectar "llamada a función dentro de un loop" tiene la misma estructura conceptual en TypeScript, Java y Python; solo cambian los nombres de los nodos de la gramática.

Esto significa que muchas reglas se escriben una vez como patrón abstracto y se parametrizan por lenguaje, en vez de reimplementarse desde cero. El sensor de N+1, por ejemplo, vive en el núcleo; cada provider solo aporta la query tree-sitter concreta de cómo se ve "una llamada al ORM dentro de un loop" en su gramática.

### Qué se escribe al agregar un lenguaje nuevo

Para incorporar, por ejemplo, Rust o Ruby, un contribuidor implementa:

1. La gramática tree-sitter (ya existe para 200+ lenguajes, solo se enchufa).
2. Las queries tree-sitter de seguridad, best practices y N+1.
3. Las reglas de best practices del ecosistema.
4. El parser de su ORM/schema al modelo común `DBSchema`.
5. El harness de benchmarking de su ecosistema.
6. La detección de su framework de tests.

**Cero cambios en el núcleo, los sensores, el MCP server, el CLI o el reporting.** Esto es lo que hace que un proyecto open source crezca solo: la comunidad puede aportar lenguajes sin entender el motor.

### Sensores agnósticos, providers concretos

Los sensores viven en el núcleo y son agnósticos al lenguaje. Orquestan las capas de la pirámide y piden datos concretos al provider activo:

```
Sensor Security (universal)
  → pide al provider sus SecurityQueries()
  → corre las queries contra el AST (provider.Grammar())
  → para los candidatos no concluyentes, invoca el LLM con provider.ReviewPromptContext()
  → emite []Finding (tipo universal)
```

El sensor no sabe si está auditando TypeScript o Java. Solo conoce la interface.

---

## 15. Optimización de rendimiento y tokens

El objetivo de esta sección es garantizar que codefit funcione a la perfección en ambos modos (CLI y MCP) sin degradarse, minimizando costo de tokens y latencia. Las justificaciones cuantitativas (números de tokens, costos, tiempos) se mantienen en un documento de análisis paralelo; aquí se especifican las técnicas como requisitos de diseño.

### Principio rector: pirámide de filtrado

**Nunca enviar al LLM lo que un filtro más barato puede descartar o resolver.** Cada capa más cara solo procesa lo que la capa anterior no pudo concluir.

| Capa | Costo | Qué resuelve | Qué pasa a la siguiente capa |
|---|---|---|---|
| 0 — Filtro de cambios | Gratis | Archivos sin cambios (vía `--since` o hash de caché) | Solo lo modificado |
| 1 — Regex / patrones | Microsegundos | Secretos, console.log, patrones obvios y concluyentes | Lo que requiere estructura |
| 2 — AST tree-sitter | Sub-ms | Estructura, tipos, N+1, missing await, best practices | Solo fragmentos sospechosos no concluyentes |
| 3 — LLM | Segundos, $$$ | Razonamiento semántico: IDOR, calidad de diseño, 3FN, review | — |

El error que comete la mayoría de las herramientas es mandar archivos enteros al LLM "para que revise". codefit manda al LLM **solo los fragmentos que el AST marcó como sospechosos pero no pudo dictaminar**. Si el AST ya determinó que hay un `any` en la línea 40, eso es un finding determinístico que no consume tokens.

### Las siete optimizaciones (requisitos de diseño)

**1. Pirámide de filtrado.** Descrita arriba. Es la optimización de mayor impacto. Reduce drásticamente el código que llega al LLM.

**2. Caché de findings por hash de contenido.** Cada archivo (y opcionalmente cada función) se hashea. Si el hash no cambió desde la última corrida, sus findings se reutilizan desde `.codefit/cache/`. En el flujo diario, donde la mayoría del proyecto no cambia entre PRs, un "full scan" recurrente cuesta prácticamente lo mismo que un incremental.

**3. Prompt caching del provider LLM.** El system prompt de cada sensor y el contexto estable del proyecto se marcan como cacheables (Anthropic prompt caching y equivalentes). Es lo que elimina el overhead del modo stateless en MCP. **Requisito desde la Fase 0 del MCP server.**

**4. Batching de archivos pequeños.** En vez de una llamada LLM por archivo, agrupar varios archivos chicos (< ~150 líneas) hasta llenar una ventana de contexto óptima (~8k tokens). Reduce el overhead fijo por llamada (system prompt repetido) y la latencia agregada.

**5. Lazy evaluation de sensores en MCP.** En modo MCP, codefit nunca corre un sensor que no se pidió. Si el agente solo llama `scan_security`, el sensor de DB no se inicializa. Garantizado por el diseño stateless: sin inicialización compartida innecesaria.

**6. Resultados parciales para cancelación temprana.** Si `scan_security` encuentra un crítico bloqueante y el orquestador está en modo "bloquear ante crítico", no tiene sentido gastar tokens en `review_code` sobre código que será reescrito. codefit expone resultados de forma incremental (streaming de findings, no todo-o-nada) para que el orquestador pueda cortar.

**7. Orden de ejecución consciente del costo.** Los sensores gratuitos (regex, AST, schema) corren antes que los de LLM. Así, si `--fail-on critical` se va a disparar por algo que detectó el AST, se puede cortar antes de gastar un solo token de LLM. El orden de ejecución es una decisión de costo, no arbitraria.

### Aplicación por modo

| Optimización | Beneficia CLI | Beneficia MCP |
|---|---|---|
| Pirámide de filtrado | ✅ | ✅ |
| Caché por hash | ✅ (corridas repetidas) | ✅ (entre sesiones) |
| Prompt caching | ✅ (sensores múltiples) | ✅✅ (crítico para stateless) |
| Batching | ✅ | ✅ |
| Lazy evaluation | parcial | ✅✅ (core del modo) |
| Cancelación temprana | ✅ (`--fail-on`) | ✅✅ (loop de auto-corrección) |
| Orden consciente del costo | ✅ | ✅ |

### Garantía de no-degradación al escalar lenguajes

Las optimizaciones viven en el núcleo (`core/pipeline`, `core/cache`, `core/llm`), no en los providers. Un lenguaje nuevo hereda automáticamente toda la pirámide de filtrado, el caché, el prompt caching y el batching sin reimplementar nada. Esto garantiza que el lenguaje #5 sea tan eficiente como el #1.

---

## 16. Sensores — Especificación detallada

*(Las reglas completas están en RF-01 a RF-06. Esta sección especifica detalles de implementación.)*

### Sensor de Seguridad — detalles de implementación

- **Capa 1 (fast, sin LLM):** Regex de alta precisión para secretos hardcodeados. Se ejecuta primero, es < 1 segundo.
- **Capa 2 (AST):** Análisis de árbol sintáctico para detectar patrones de inyección, missing auth, criptografía débil.
- **Capa 3 (LLM, opcional):** Para hallazgos que requieren comprensión semántica: IDOR, over-fetching de datos sensibles, lógica de autorización incorrecta.
- **Dependencias:** Análisis de `package.json`/`pom.xml`/`requirements.txt` contra advisory databases públicas (GitHub Advisory Database, OSV).

### Sensor DB — detalles de implementación

- Soporta lectura de: `schema.prisma`, `*.sql` migrations, anotaciones TypeORM/Hibernate, modelos SQLAlchemy.
- Detección automática de paradigma OLTP vs OLAP por heurísticas de naming y estructura.
- Las reglas de índices requieren el schema + análisis del código fuente (queries y llamadas ORM) para detectar columnas frecuentemente filtradas.
- Las reglas de vistas, procedimientos y triggers requieren el schema SQL completo.

### Sensor de Tests — riesgo de regresión

El análisis de riesgo de regresión en modo `--since` produce un reporte adicional:

```
RIESGO DE REGRESIÓN (modo incremental)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Archivos modificados: 3
Funciones modificadas: 7

ALTO RIESGO:
  src/services/auth.ts#validateToken()
  → Función pública modificada, usada en 12 callsites
  → 0 tests cubren esta función
  → Callsites afectados: routes/plants.ts:45, routes/users.ts:23, ...

MEDIO RIESGO:
  src/utils/formatDate.ts#formatISO()
  → Utility modificada, usada en 8 callsites
  → 2 tests existentes (cobertura parcial)

SIN RIESGO DETECTADO:
  src/components/PlantCard.tsx (tests presentes y actualizados)
```

---

## 17. Sandbox de ejecución

### Requisito: Docker

El sensor de complejidad requiere Docker. Sin Docker, se omite con advertencia clara. El resto funciona sin Docker.

### Especificación del contenedor

```
Imagen base:  node:20-alpine (TS) | eclipse-temurin:21-alpine (Java) | python:3.12-slim (Python)
Red:          --network none
CPU:          --cpus 1
Memoria:      --memory 512m
Filesystem:   solo lectura excepto /tmp
Timeout:      configurable, default 30s por ejecución individual
Cleanup:      --rm (autoeliminación)
```

Las funciones auditadas no pueden conectarse a la DB real ni a servicios externos. Los datos de prueba vienen 100% del generador provisto por el usuario.

---

## 18. Sistema de reporte

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
      "security": 41,
      "review": 72,
      "db": 58,
      "complexity": 81,
      "tests": 71
    }
  },
  "blocked": true,
  "block_reason": "Hallazgos críticos de seguridad sin consentimiento explícito",
  "baseline": {
    "active": true,
    "new_findings": 3,
    "baselined_findings": 47
  },
  "findings": [ ... ],
  "regression_risk": { ... },
  "sensor_results": [ ... ]
}
```

### Campo `schema_version`

El JSON declara su `schema_version` separado de la versión de codefit. Las herramientas que consumen el JSON (dashboards, integraciones de la comunidad, CI) dependen de un contrato estable. Cuando se agreguen campos o se cambien estructuras en el futuro, `schema_version` permite que esos consumidores no se rompan y migren de forma controlada. Es un requisito desde la Fase 0.

### Campo `blocked`

Si hay hallazgos críticos de seguridad sin consentimiento registrado, el campo `blocked: true` activa el exit code 1 **independientemente** del flag `--fail-on`. Este comportamiento no es configurable: la seguridad crítica siempre bloquea. Los findings marcados como `baselined: true` (deuda histórica) no activan el bloqueo.

### Renderers y detección de TTY

El JSON canónico es la única fuente de verdad. Sobre él se montan tres renderers intercambiables, sin que el núcleo sepa cuál se usa:

| Renderer | Cuándo | Uso |
|---|---|---|
| **Plano (stdout)** | Sin TTY (pipe, CI/CD, hook) o `--no-tui` | Texto simple, parseable, ideal para pipelines |
| **TUI (interactivo)** | Con TTY y `--tui` o por defecto en terminal interactiva | Exploración con teclado (ver roadmap, sección 22) |
| **HTML standalone** | `--output html` | Reporte compartible |

**Regla de detección de TTY:** codefit detecta automáticamente si su salida está conectada a una terminal interactiva. Si la salida está siendo pipeada o no hay TTY (caso típico de CI/CD y MCP), usa el renderer plano sin importar la configuración. La TUI nunca se fuerza en contextos no interactivos, lo que garantiza que codefit siga funcionando en pipelines. Flags `--tui` / `--no-tui` permiten override manual.

Esta separación es la razón por la que la TUI (sección 22, roadmap) se puede agregar después sin tocar el núcleo: es solo un renderer más sobre el mismo JSON.

---

## 19. Modos de integración

### Proyecto nuevo

```bash
codefit auth login
codefit init
git add .codefit.yaml
codefit run
```

### Proyecto en curso (sin interrumpir el flujo)

1. `codefit init --non-interactive` → genera config base
2. `codefit run` → primera auditoría: establece línea base, no bloquea nada
3. Revisar `.codefit.yaml`, ajustar umbrales, ignorar falsos positivos con justificación
4. De ahí en adelante: `codefit scan --since HEAD~1` en cada feature branch

### CI/CD — GitHub Actions

```yaml
name: codefit quality gate
on:
  pull_request:
    branches: [main]

jobs:
  audit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Install codefit
        run: |
          curl -sSL https://github.com/codefit-cli/codefit/releases/latest/download/codefit-linux-amd64.tar.gz | tar xz
          sudo mv codefit /usr/local/bin/

      - name: Security + static audit
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
        run: codefit scan --since origin/main --output markdown

      - name: Benchmarks (en PRs que modifican lógica crítica)
        if: contains(github.event.pull_request.labels.*.name, 'performance-sensitive')
        run: codefit bench
```

### Pre-commit hook (rápido, sin LLM)

```bash
#!/bin/sh
# Solo seguridad sin LLM — corre en < 3 segundos
codefit scan --sensor security --since HEAD --no-llm --fail-on critical --quiet
```

### Integración con el ecosistema gentle-ai / SDD

```
Phase 1–7:  Diseño y generación (gentle-ai / OpenCode / Claude Code)
                ↓
Phase 8:    codefit scan --sensor security   ← bloqueante inmediato
                ↓
Phase 9:    codefit scan (estático completo)
                ↓
Phase 10:   codefit bench (si hay funciones críticas)
                ↓
Deploy / merge
```

---

## 20. Soporte de plataformas y lenguajes

### Plataformas (day 1)

| OS | Arquitectura | Soporte | Notas |
|---|---|---|---|
| Linux | x86_64 | ✅ | Target primario |
| Linux | arm64 | ✅ | Cross-compile con goreleaser |
| Windows | x86_64 | ✅ | Binario nativo. Docker Desktop para benchmarks |
| Windows WSL2 | x86_64 | ✅ | Funciona como Linux |
| macOS | arm64 | 🔶 | Incluido en goreleaser, no testeado activamente en v1 |

### Lenguajes target por fase

| Lenguaje / Ecosistema | Fase | Sensores | Nota |
|---|---|---|---|
| **Go** | v1.0 (Fase 0) | Seguridad, Code Review, Best Practices, Tests | Provider de arranque: codefit se audita a sí mismo desde el primer commit |
| TypeScript / React / Next.js | v1.0 | Seguridad, Code Review, DB, Complejidad, Best Practices, Tests | Primer target de producto completo |
| Java / Spring | v1.1 | Seguridad, Code Review, DB, Complejidad, Best Practices, Tests | |
| Python / FastAPI / Django | v1.2 | Seguridad, Code Review, DB, Complejidad, Best Practices, Tests | |

**Por qué Go de entrada:** codefit está escrito en Go. Tener el `LanguageProvider` de Go desde el día 1 significa que codefit se audita a sí mismo en cada PR (self-dogfooding). Cada falso positivo que encuentra en su propio código se convierte en una corrección inmediata. Es el mejor caso de prueba posible — real, permanente y gratis — y un argumento de marketing potente para el open source: "la herramienta que se audita a sí misma". El provider de Go en Fase 0 es más simple que el de TS (no necesita sensor de DB ni de complejidad para arrancar), lo que lo hace ideal como primer ejercicio de la interface `LanguageProvider`.

### Bases de datos y paradigmas soportados (v1.0)

| DB | OLTP | OLAP | Schema source | ORM |
|---|---|---|---|---|
| PostgreSQL | ✅ | ✅ | .sql migrations | Prisma, TypeORM, Drizzle |
| SQLite | ✅ | — | .sql, schema.prisma | Prisma |
| MySQL | ✅ | — | .sql migrations | TypeORM, Prisma |

---

## 21. Rollout por fases

### Fase 0 — Foundations + Arquitectura del núcleo + Go Provider `(~2 semanas)`
- Repo Go, estructura de tres capas (`core/`, `sensors/`, `providers/`), CI propio con GitHub Actions
- Interface `Sensor` y tipos base (`Finding` con `reasoning`/`baselined`, `ConsentRecord`, `AuditContext`)
- **Interface `LanguageProvider`** definida (el contrato de extensibilidad)
- **Go `LanguageProvider`** (provider de arranque): gramática tree-sitter de Go, queries base. Permite que codefit empiece a auditarse a sí mismo de inmediato.
- Integración de **tree-sitter puro Go (sin CGO)** verificada con cross-compile a Linux y Windows
- Núcleo: `core/pipeline` (pirámide de filtrado capas 0-2), `core/cache` (hash de contenido), `core/llm` (cliente abstracto + prompt caching), `core/scoring`, `core/report` (con `schema_version` y detección de TTY)
- Renderer plano (stdout) funcional; JSON canónico con `schema_version`
- Soporte de `path_criticality` (RF-11) en el núcleo
- CLI skeleton: todos los subcomandos y flags definidos (incluido `baseline`)
- Parser y validador de `.codefit.yaml`
- Sistema de auth: `codefit auth login` (wizard), keychain (go-keyring), fallback archivo cifrado
- `codefit set model`, `codefit auth status`, `codefit status`
- Docker sandbox básico: levantar, ejecutar, destruir
- **CI corre `codefit scan` sobre el propio repo** (self-dogfooding desde el primer PR)

**Done cuando:** `codefit auth login` funciona en Linux y Windows con binario único (cross-compile sin toolchain de C). El CI de codefit se audita a sí mismo usando el Go provider. La interface `LanguageProvider` está definida e implementada por Go.

### Fase 1 — TypeScript Provider + Sensor de Seguridad `(~3 semanas)` *(prioridad máxima)*
- **TypeScript `LanguageProvider`**: gramática tree-sitter, queries base, defaults de `path_criticality`
- Capa 1: detección de secretos hardcodeados (regex de alta precisión)
- Capa 2: análisis AST para inyección, missing auth, criptografía débil (React/TS)
- Capa 3: LLM para IDOR y over-fetching (con prompt caching, pirámide de filtrado y campo `reasoning` en findings)
- Análisis de dependencias con CVEs (GitHub Advisory Database)
- Sistema de consentimiento explícito para critical security
- **Comando `baseline`** (RF-10) funcional

**Done cuando:** `codefit scan --sensor security` detecta al menos 3 categorías distintas de vulnerabilidades en proyectos de prueba, con zero falsos positivos en la detección de secretos hardcodeados. El LLM solo recibe fragmentos sospechosos no concluyentes (pirámide funcionando). El sensor de seguridad corre sobre Go (self-audit) y TypeScript.

### Fase 2 — MCP Server `(~1 semana)`
- `codefit mcp serve` (stdio transport)
- Adapter de tools MCP → núcleo (sin reimplementar lógica)
- Tools: `scan_security`, `scan_all`, `describe_rules` (las que dependen de sensores ya construidos)
- Modelo stateless con prompt caching verificado
- Configuración de ejemplo para Claude Code y OpenCode

**Done cuando:** un agente en OpenCode puede llamar `mcp__codefit__scan_security` durante una sesión y recibir findings JSON. El overhead de tokens del stateless está mitigado por prompt caching.

### Fase 3 — Sensor de DB `(~2 semanas)`
- `ParseSchema` del TypeScript provider: `schema.prisma` y SQL migrations
- Reglas OLTP: FKs, índices (faltantes, duplicados, redundantes), tablas sin PK
- Reglas de vistas, procedimientos y triggers
- Detección automática OLTP vs OLAP + reglas OLAP básicas
- Detección N+1 en código TypeScript (query tree-sitter del provider)
- Inferencia 3FN via LLM para OLTP
- Tool MCP `scan_db` habilitada

**Done cuando:** `codefit scan --sensor db` en PlantaLinda genera hallazgos reales verificados manualmente.

### Fase 4 — Code Review + Best Practices + Tests `(~2 semanas)`
- Sensor code review (LLM con contexto de proyecto, chunking 500/50, batching)
- Sensor best practices (AST para React/TS via queries del provider)
- Sensor tests: detección de ausencia, tests sin assertions, cobertura de funciones críticas
- Análisis de riesgo de regresión en modo `--since`
- Tools MCP `review_code`, `check_practices`, `scan_tests` habilitadas

**Done cuando:** `codefit review --since origin/main` produce un code review accionable en un PR real de PlantaLinda.

### Fase 5 — Complejidad Algorítmica `(~3 semanas)`
- `BuildHarness` del TypeScript provider (tsx, sin compilación)
- Generador de inputs: protocolo y validación
- `core/complexity`: regresión de curvas (implementación Go propia, universal)
- Finding con best fit y R²
- Tool MCP `bench_function` habilitada

**Done cuando:** `codefit bench --function searchPlants` clasifica correctamente la complejidad de una función real.

### Fase 6 — Reporte + Release público `(~1 semana)`
- Renderer HTML standalone
- Score global con pesos configurables
- `codefit report --output html`
- goreleaser: binarios Linux x86_64, Linux arm64, Windows x86_64
- README con ejemplos reales (dogfoodeados en PlantaLinda)
- GitHub Actions template para el usuario + acción `codefit-cli/action`
- Estructura de comunidad: CONTRIBUTING.md, SECURITY.md, CODE_OF_CONDUCT.md, templates
- Licencia Apache 2.0, tag v0.1.0

**Done cuando:** un usuario externo puede instalar codefit, correr `codefit run` sobre su proyecto TypeScript, y opcionalmente conectarlo a su agente vía MCP.

### Post-v1.0 — Escalado de lenguajes
- **v1.1:** Java `LanguageProvider` (Spring). Solo se implementa el provider; el núcleo no cambia.
- **v1.2:** Python `LanguageProvider` (FastAPI / Django). Idem.

Cada lenguaje nuevo es una validación del diseño de extensibilidad: si agregar Java requiere tocar el núcleo, el diseño falló.

---

## 22. Roadmap futuro / Ideas

Funcionalidades evaluadas y aprobadas conceptualmente, planificadas para después de v1.0. No afectan la arquitectura del núcleo (por diseño, se agregan como capas sobre el JSON canónico o como providers), por lo que se difieren sin riesgo.

### TUI interactiva (renderer)

Una interfaz de terminal interactiva para explorar findings, construida sobre Bubble Tea + Lipgloss (Charm). Se activa automáticamente cuando hay TTY; cae al renderer plano en CI/MCP. Es un renderer más sobre el JSON canónico, no toca el núcleo.

Funcionalidad prevista: navegación con teclado por findings, filtrado por severidad/dimensión, snippet de código inline, y generación de bloques de `ignore`/`consent` directamente al `.codefit.yaml` desde la TUI.

```
┌─ codefit · plantalinda-api ──────────── Score: 64/100 ─┐
│ 🔒 Security  41 ░░░░  ⚡ Complex 81 ███░  🧪 Tests 71 ██░│
├────────────────┬───────────────────────────────────────┤
│ ▸ 🔴 SEC-001   │  SEC-001 · JWT secret hardcodeado     │
│   🔴 SEC-021   │  src/auth/jwt.ts:8                     │
│   🟠 DB-001    │  const SECRET = "hardcoded-key-123"    │
│   🟡 REV-014   │  → Mover a process.env.JWT_SECRET      │
│                │  [i] ignorar  [c] consent  [r] razón   │
└────────────────┴───────────────────────────────────────┘
   ↑↓ navegar · enter detalle · / filtrar · q salir
```

**Justificación de diferir:** codefit es fundamentalmente una herramienta de pipeline. El renderer plano + JSON deben estar sólidos primero. La TUI es UX adicional que se agrega sin reescribir nada gracias a la separación de renderers (sección 18).

### Modo watch

`codefit watch` corre en background, observa cambios en archivos y audita incrementalmente lo que se modifica, en tiempo real. Solo capas baratas (regex + AST, sin LLM) para feedback instantáneo. Complementa al modo MCP: para quien no usa un agente, el watch da parte del beneficio de la auditoría continua.

### Self-audit como bandera de calidad

codefit auditándose a sí mismo (habilitado desde Fase 0 con el Go provider) se convierte en un argumento de marketing y en un mecanismo de mejora continua: cada falso positivo en el propio código de codefit se corrige de inmediato. A medida que el proyecto madura, se publica el reporte de auto-auditoría como señal de confianza ("the tool that audits itself").

### Telemetría opt-in y anónima

Para priorizar bien (qué sensores se usan, qué reglas generan más ignores = señal de falso positivo, qué lenguajes), telemetría **opt-in explícita, anónima y transparente**, documentada en el README, que nunca envía código ni paths. Por la sensibilidad de la comunidad open source a este tema, se difiere hasta tener tracción y se discute públicamente antes de implementarla.

### Otras ideas en evaluación

- Renderer SARIF (formato estándar de findings) para integración nativa con GitHub Code Scanning.
- Plugin de sugerencias de fix automáticas (capa generativa opcional, claramente separada de la auditoría).
- Dashboard web que consume múltiples reportes JSON a lo largo del tiempo (evolución del score).

---

## 23. Métricas de éxito

### Calidad del producto
- Tasa de falsos positivos en detección de secretos hardcodeados: **objetivo < 1%**
- Tasa de falsos positivos en reglas DB determinísticas: **objetivo 0%**
- Precisión de clasificación de complejidad vs. análisis manual: **objetivo > 85%**
- Tiempo de `codefit scan --no-llm` en proyecto mediano: **objetivo < 10 segundos**

### Adopción
- GitHub stars en el primer mes post-release
- Proyectos con `.codefit.yaml` commiteado (medible via GitHub code search)
- Issues y PRs de la comunidad

### Dogfooding
- `codefit` se ejecuta sobre PlantaLinda desde Fase 1
- Cualquier hallazgo de seguridad o DB detectado en PlantaLinda que no hubiera sido identificado manualmente = validación de valor real

---

## 24. Decisiones resueltas

Todas las decisiones arquitectónicas y de producto están cerradas. No hay pendientes para comenzar la Fase 0.

| # | Decisión | Resolución |
|---|---|---|
| 1 | Nombre del binario y proyecto | ✅ **`codefit`** |
| 2 | Licencia | ✅ **Apache 2.0** |
| 3 | Repositorio y organización GitHub | ✅ **`github.com/codefit-cli/codefit`** |
| 4 | Módulo Go | ✅ **`github.com/codefit-cli/codefit`** |
| 5 | Modelo de distribución | ✅ **GitHub Releases via goreleaser** |
| 6 | Advisory DB para CVEs | ✅ **GitHub Advisory Database** (open, sin API key requerida) |
| 7 | Harness TypeScript | ✅ **tsx** (sin paso de compilación, overhead mínimo, ideal para MVP) |
| 8 | Regresión de curvas de complejidad | ✅ **Implementación propia en Go** (cero dependencias externas, binario único) |
| 9 | Auth LLM | ✅ **API key directa del usuario** almacenada en keychain del SO. Portal propio fuera del scope. |
| 10 | Chunking para code review LLM | ✅ **500 líneas por chunk** con solapamiento de 50 líneas. Contexto de archivo completo para archivos ≤ 500 líneas. |
| 11 | Lenguaje de implementación | ✅ **Go** (binario único, cross-compile, comunidad). El cuello de botella es LLM/IO, no CPU; Rust no aporta ventaja relevante. |
| 12 | Parsing AST | ✅ **tree-sitter puro Go, sin CGO** (preserva binario único y cross-compile; más rápido en parsing incremental para `--since`) |
| 13 | Arquitectura de extensibilidad | ✅ **Núcleo universal + LanguageProvider**. Agregar un lenguaje no toca el núcleo. |
| 14 | Modos de operación | ✅ **CLI y MCP** sobre el mismo núcleo. MCP stateless. |
| 15 | Estado en MCP | ✅ **Stateless** con prompt caching para mitigar overhead. El orquestador acumula findings. |
| 16 | Optimización de tokens | ✅ **Pirámide de filtrado** (capas 0-3) + caché por hash + prompt caching + batching. El LLM solo procesa lo no concluyente. |
| 17 | Transport MCP | ✅ **stdio por defecto**, HTTP/SSE opcional (`--port`) para clientes remotos |
| 18 | Provider de arranque | ✅ **Go**, desde Fase 0. codefit se audita a sí mismo (self-dogfooding) desde el primer commit. |
| 19 | Adopción de proyectos existentes | ✅ **Baseline** (RF-10): solo reporta findings nuevos, la deuda histórica no genera ruido. |
| 20 | Severidad contextual | ✅ **path_criticality** (RF-11): pondera severidad según production/test/example. |
| 21 | Renderers | ✅ **Tres renderers sobre JSON canónico** con detección automática de TTY: plano, TUI, HTML. TUI diferida a post-v1.0. |
| 22 | Compatibilidad del JSON | ✅ **schema_version** en el reporte desde Fase 0, independiente de la versión de codefit. |
| 23 | Explicabilidad | ✅ Campo **reasoning** en findings probabilísticos (LLM) para construir confianza. |

---

## 25. GitHub y open source

### Identidad del proyecto

| Atributo | Valor |
|---|---|
| Nombre | codefit |
| Organización GitHub | `github.com/codefit-cli` |
| Repositorio principal | `github.com/codefit-cli/codefit` |
| Módulo Go | `github.com/codefit-cli/codefit` |
| Licencia | Apache 2.0 |
| Binario | `codefit` |
| Config file | `.codefit.yaml` |
| Config global | `~/.config/codefit/config.yaml` |

### Estructura del repositorio en GitHub

```
github.com/codefit-cli/codefit/
├── .github/
│   ├── workflows/
│   │   ├── ci.yml              # Tests, lint, build en cada PR
│   │   ├── release.yml         # goreleaser en cada tag vX.Y.Z
│   │   └── security.yml        # Escaneo de dependencias del propio repo
│   ├── ISSUE_TEMPLATE/
│   │   ├── bug_report.md
│   │   ├── feature_request.md
│   │   └── false_positive.md   # Template específico para falsos positivos de sensores
│   └── PULL_REQUEST_TEMPLATE.md
├── CONTRIBUTING.md             # Cómo contribuir: setup, convenciones, PRs
├── CODE_OF_CONDUCT.md          # Contributor Covenant
├── SECURITY.md                 # Cómo reportar vulnerabilidades en codefit mismo
├── CHANGELOG.md                # Versionado semántico, mantenido con git-cliff
├── LICENSE                     # Apache 2.0
├── README.md
└── ... (código fuente)
```

### Modelo de contribución

**Versioning:** Semantic Versioning (SemVer). `v0.x` durante desarrollo pre-release. `v1.0.0` marca estabilidad de la interfaz CLI y el contrato del `.codefit.yaml`.

**Branching:** `main` siempre deployable. Features en `feature/*`. Fixes en `fix/*`. Releases desde tags.

**CI propio:** Antes de aceptar cualquier PR, el workflow de CI corre:
- `go test ./...` con race detector
- `golangci-lint`
- Build cross-platform (Linux x86_64, Linux arm64, Windows x86_64)
- **`codefit scan`** sobre el propio código del PR (dogfooding automático)

**Release process:** Al tagear `vX.Y.Z`, goreleaser compila los binarios, genera checksums, crea la GitHub Release con el CHANGELOG del período, y publica los assets descargables.

**GitHub Discussions:** Habilitadas desde el día 1 para:
- Q&A (dudas de uso, configuración)
- Ideas (propuestas de nuevos sensores, integraciones)
- Show & tell (proyectos que usan codefit)

**Issues:** Etiquetas estándar más `sensor:security`, `sensor:db`, `sensor:complexity`, `sensor:review`, `sensor:tests`, `false-positive`, `lang:typescript`, `lang:java`, `lang:python`.

### Instalación para el usuario final

**Linux / macOS (curl):**
```bash
curl -sSL https://github.com/codefit-cli/codefit/releases/latest/download/install.sh | sh
```

**Linux / macOS (go install):**
```bash
go install github.com/codefit-cli/codefit/cmd/codefit@latest
```

**Windows (PowerShell):**
```powershell
winget install codefit.codefit
```

**GitHub Actions (sin instalación previa):**
```yaml
- uses: codefit-cli/action@v1
  with:
    fail-on: critical
```

### Roadmap público

El roadmap se gestiona como GitHub Project en la organización `codefit`. Las fases del PRD se mapean como Milestones:

| Milestone | Contenido | Release |
|---|---|---|
| `v0.1.0` | Fase 0 + Fase 1 (Foundations + Núcleo + Go Provider + TS Provider + Security) | Primera release pública |
| `v0.2.0` | Fase 2 (MCP Server) | — |
| `v0.3.0` | Fase 3 (DB sensor) | — |
| `v0.4.0` | Fase 4 (Code Review + Best Practices + Tests) | — |
| `v0.5.0` | Fase 5 (Complejidad algorítmica) | — |
| `v1.0.0` | Fase 6 completa + estabilidad de interfaz CLI/MCP | Release estable |
| `v1.1.0` | Java LanguageProvider | — |
| `v1.2.0` | Python LanguageProvider | — |

---

## 26. Glosario

| Término | Definición |
|---|---|
| **Sensor** | Módulo que implementa la interface `Sensor` y produce `[]Finding` para una dimensión específica. |
| **Finding** | Hallazgo individual. Tiene severidad, dimensión, ubicación, descripción y recomendación. |
| **ConsentRecord** | Registro de que un hallazgo crítico de seguridad fue revisado y aceptado explícitamente. |
| **Harness** | Código auxiliar que envuelve la función auditada para permitir su benchmarking en el sandbox. |
| **Sandbox** | Contenedor Docker efímero, sin red, con recursos limitados. |
| **Generador** | Función provista por el usuario que produce un input válido de tamaño n para una función objetivo. |
| **Best fit** | La curva de complejidad más simple cuyo R² supera el umbral mínimo de confianza. |
| **Incremental / diff-based** | Modo donde solo se analizan archivos modificados desde un commit de referencia. |
| **AuditContext** | Struct Go que encapsula toda la información del proyecto y se pasa a cada sensor. |
| **OLTP** | Online Transaction Processing. Paradigma transaccional, aplica reglas de normalización. |
| **OLAP** | Online Analytical Processing. Paradigma analítico (DW, Data Marts). Desnormalización intencional. |
| **Riesgo de regresión** | Evaluación de qué funcionalidades existentes pueden verse afectadas por cambios recientes. No ejecuta tests. |
| **Probabilístico** | Finding cuya confianza < 1.0, generado por inferencia LLM. Siempre marcado como tal. |
| **Keychain** | Almacenamiento seguro de credenciales provisto por el SO (Credential Manager en Windows, libsecret en Linux). |
| **MCP** | Model Context Protocol. Estándar para exponer herramientas a agentes de IA. codefit lo implementa como servidor. |
| **MCP Server** | Modo de operación donde codefit expone sus sensores como herramientas que un agente orquestador puede llamar. |
| **Orquestador** | El agente de IA (ej: Claude Opus en OpenCode) que dirige el flujo y llama las herramientas MCP de codefit. No es parte de codefit. |
| **Stateless** | Diseño donde cada llamada MCP es independiente, sin memoria de llamadas previas. El orquestador acumula el estado. |
| **Núcleo / core** | Capa universal de codefit, agnóstica al lenguaje: orquestación, pirámide, caché, scoring, reporting, LLM. |
| **LanguageProvider** | Interface que cada lenguaje implementa (queries tree-sitter, reglas, harness, parser de schema). Permite escalar sin tocar el núcleo. |
| **Pirámide de filtrado** | Estrategia de optimización: capas baratas (filtro de cambios, regex, AST) resuelven primero; el LLM solo procesa lo no concluyente. |
| **Prompt caching** | Técnica que cachea el system prompt y contexto estable entre llamadas LLM, eliminando el overhead del modo stateless. |
| **tree-sitter** | Librería de parsing incremental. codefit usa una implementación pura en Go (sin CGO) para preservar el binario único. |
| **Baseline** | Foto del estado de findings de un proyecto. Permite reportar solo findings nuevos e ignorar la deuda histórica (RF-10). |
| **path_criticality** | Clasificación de directorios (production/test/example) que pondera la severidad de los findings según su ubicación (RF-11). |
| **schema_version** | Versión del formato del JSON canónico, independiente de la versión de codefit. Garantiza compatibilidad de consumidores. |
| **reasoning** | Campo de un finding probabilístico que explica por qué el LLM lo marcó. Construye confianza en los findings. |
| **TTY** | Terminal interactiva. codefit detecta su presencia para elegir entre renderer plano (sin TTY) y TUI (con TTY). |
| **TUI** | Text User Interface. Renderer interactivo de terminal (Bubble Tea) para explorar findings. Diferido a post-v1.0. |
| **Self-dogfooding** | codefit auditándose a sí mismo vía el Go provider, desde el primer commit. Mejor caso de prueba y señal de calidad. |
| **Renderer** | Capa de presentación sobre el JSON canónico (plano, TUI, HTML). El núcleo es agnóstico al renderer usado. |
