# codefit — Análisis objetivo: tokens, tiempos, costos, pros y contras

**Documento de referencia arquitectónica**
**Fecha:** Junio 2026

---

## Escenarios de referencia

Para que los números sean comparables, definimos tres tamaños de proyecto TypeScript + PostgreSQL:

| Escenario | Líneas de código | Archivos TS | Tablas DB | Uso típico |
|---|---|---|---|---|
| **Small** | 2k–5k | 20–40 | 10–20 | Feature nueva, MVP, microservicio |
| **Medium** | 10k–30k | 80–150 | 30–80 | SaaS en crecimiento (ej: PlantaLinda) |
| **Large** | 50k–100k | 300–600 | 100–300 | Producto maduro, monorepo |

---

## 1. Consumo de tokens por sensor

### Qué genera tokens y qué no

| Sensor | Capa | LLM necesario | Tokens input | Tokens output |
|---|---|---|---|---|
| Security: regex/AST | Estático | ❌ No | 0 | 0 |
| Security: semántico | LLM | ✅ Sí | ~4k–6k por archivo crítico | ~800–1.5k |
| Code Review | LLM | ✅ Sí | ~6k–9k por chunk (500 líneas) | ~1k–2k |
| DB: schema estático | Estático | ❌ No | 0 | 0 |
| DB: inferencia 3FN | LLM | ✅ Sí | ~5k–15k por schema | ~800–1.5k |
| Best Practices | AST | ❌ No | 0 | 0 |
| Tests: detección | Estático | ❌ No | 0 | 0 |
| Tests: riesgo regresión | LLM | ✅ Sí | ~3k–6k por batch de archivos | ~800–1.2k |
| Complejidad algorítmica | Ejecución | ❌ No (Docker) | 0 | 0 |
| Summary ejecutivo | LLM | Opcional | ~3k–6k (findings JSON) | ~500–1k |

### Tokens totales por escenario (full scan, todos los sensores LLM activos)

| Sensor LLM | Small | Medium | Large |
|---|---|---|---|
| Security semántico | ~25k input / 5k output | ~75k / 15k | ~200k / 40k |
| Code Review | ~40k input / 10k output | ~200k / 45k | ~1.000k / 220k |
| DB inferencia 3FN | ~8k input / 1.5k output | ~20k / 3k | ~60k / 10k |
| Riesgo regresión | ~5k input / 1k output | ~12k / 2.5k | ~30k / 6k |
| Summary | ~4k input / 0.8k output | ~6k / 1k | ~8k / 1.5k |
| **TOTAL** | **~82k / ~18k** | **~313k / ~66k** | **~1.298k / ~277k** |

### Tokens en modo incremental (--since HEAD~1, ~3-5 archivos cambiados)

| Sensor LLM | Tokens input | Tokens output |
|---|---|---|
| Security semántico (3 archivos) | ~15k | ~3k |
| Code Review (3 archivos, ~6 chunks) | ~48k | ~10k |
| DB (si no cambió el schema) | 0 | 0 |
| Riesgo regresión | ~5k | ~1k |
| **TOTAL INCREMENTAL** | **~68k** | **~14k** |

**El modo incremental usa ~5x menos tokens que el full scan en proyectos medium.**

---

## 2. Análisis de costos (precios de referencia 2026)

### Modelos evaluados

| Modelo | Input ($/MTok) | Output ($/MTok) | Calidad review | Velocidad |
|---|---|---|---|---|
| claude-sonnet-4-6 | $3.00 | $15.00 | ⭐⭐⭐⭐⭐ | Media |
| claude-haiku-4-5 | $0.25 | $1.25 | ⭐⭐⭐ | Rápida |
| Ollama / Qwen3:30b | $0.00 API | $0.00 API | ⭐⭐⭐ | Depende del hardware |
| Ollama / Llama3.3:70b | $0.00 API | $0.00 API | ⭐⭐⭐⭐ | Lenta en CPU |

### Costo por configuración (full scan)

**Configuración A — Todo Sonnet (máxima calidad)**

| Escenario | Input cost | Output cost | Total |
|---|---|---|---|
| Small | 82k × $3/M = $0.25 | 18k × $15/M = $0.27 | **$0.52** |
| Medium | 313k × $3/M = $0.94 | 66k × $15/M = $0.99 | **$1.93** |
| Large | 1.298k × $3/M = $3.89 | 277k × $15/M = $4.16 | **$8.05** |

**Configuración B — Routing inteligente (Sonnet para security+review, Haiku para DB+regresión+summary)**

| Escenario | Sonnet cost | Haiku cost | Total | Ahorro vs A |
|---|---|---|---|---|
| Small | $0.47 | $0.003 | **$0.47** | 10% |
| Medium | $1.82 | $0.009 | **$1.83** | 5% |
| Large | $7.72 | $0.027 | **$7.75** | 4% |

> **Insight:** El routing de modelos ahorra poco porque code review domina el 85%+ del costo total. El único ahorro real es usar Haiku donde la calidad no importa tanto (DB, summary).

**Configuración C — Solo sensores sin LLM (--no-llm)**

| Escenario | Costo |
|---|---|
| Small / Medium / Large | **$0.00** |

Detecta: secretos hardcodeados (regex), FK sin índice, N+1, best practices (AST), columnas multivaluadas.
No detecta: calidad de diseño, violaciones 3FN semánticas, code review profundo.

**Configuración D — Incremental (--since HEAD~1, ~5 archivos)**

| Modelo | Costo por PR |
|---|---|
| Sonnet (todo) | **~$0.33** |
| Routing B | **~$0.31** |

**Configuración E — Ollama local (sin costo API)**

| Costo API | Costo real |
|---|---|
| $0.00 | Electricidad + amortización hardware GPU |

Con RTX 4090 (referencia): ~$0.002–$0.005 por scan en electricidad.
Con hardware de servidor: mayor.

### Costo mensual estimado para un developer activo (1 PR/día, 22 días laborables)

| Configuración | Costo mensual |
|---|---|
| Full scan Sonnet (Medium project) | ~$42/mes |
| Incremental Sonnet (1 PR/día) | ~$7/mes |
| Incremental + 1 full scan semanal | ~$15/mes |
| --no-llm siempre | $0 |
| Ollama local | ~$0 API |

---

## 3. Análisis de tiempos

### Tiempos por componente

| Componente | Tiempo |
|---|---|
| Sensores estáticos (regex, AST, schema) | 1–8 seg según tamaño |
| Llamada API LLM (latencia first token) | 0.5–2 seg |
| Generación LLM por chunk (Sonnet) | 3–8 seg |
| Generación LLM (Haiku, misma tarea) | 1–3 seg |
| Ollama local (Qwen3:30b, primer token) | 2–5 seg |
| Ollama local (generación completa) | 8–25 seg por chunk |
| Docker sandbox startup | 3–6 seg |
| Benchmark por función (5 n_values × 5 runs) | 30–120 seg |

### Tiempo total por escenario

| Escenario | --no-llm | Full scan (Sonnet, paralelo) | Full scan (Sonnet, serial) | Incremental |
|---|---|---|---|---|
| Small | 1–3 seg | 12–25 seg | 35–80 seg | 8–18 seg |
| Medium | 3–8 seg | 25–60 seg | 90–220 seg | 10–22 seg |
| Large | 8–20 seg | 60–180 seg | 400–900 seg | 12–25 seg |

> Los tiempos "paralelo" asumen que todos los chunks de todos los sensores se envían concurrentemente al LLM API. La API de Anthropic tiene rate limits que pueden serializar en proyectos grandes.

### Benchmarks de complejidad (codefit bench)

| Configuración | Tiempo |
|---|---|
| 1 función, Docker startup + 5 n × 5 runs | 1–3 min |
| 5 funciones (paralelo) | 3–8 min |
| 10 funciones (paralelo) | 6–15 min |

Los benchmarks son el componente más lento por orden de magnitud. Por eso van separados en `codefit bench` y no en `codefit scan`.

---

## 4. Comparación de modos: CLI vs MCP

### Desde la perspectiva del developer/orquestador

| Dimensión | CLI manual | MCP (agente llama tools) |
|---|---|---|
| **¿Cuándo corre?** | Cuando el developer se acuerda | En cada generación de código |
| **Fricción** | Alta (cambiar contexto, abrir terminal) | Cero (el agente lo hace) |
| **Cobertura** | Variable (puede olvidarse) | Sistemática si el perfil lo define |
| **Tokens extra para el orquestador** | 0 (el dev lee el reporte) | ~2k–5k tokens (findings en contexto del agente) |
| **Costo adicional** | Solo codefit | codefit + overhead en contexto del orquestador |
| **Latencia percibida** | Asíncrona (el dev corre cuando quiere) | Síncrona (el agente espera el resultado) |
| **Granularidad** | Proyecto o diff completo | Archivo por archivo, en tiempo real |
| **Debugging de falsos positivos** | Fácil (el dev lee y decide) | El agente debe saber interpretarlos |

### Costo total de una sesión SDD con MCP vs sin MCP

Sesión típica: implementar un endpoint (1 iteración sin errores):

**Sin MCP (CLI al final):**
- Tokens orquestador SDD: ~50k–100k
- Tokens codefit (1 scan al final): ~68k input / 14k output (incremental)
- Costo codefit: ~$0.31
- Costo SDD session: ~$0.30–$0.60
- **Total: ~$0.61–$0.91**

**Con MCP (codefit se llama 2 veces: tras implementar y tras corregir):**
- Tokens orquestador SDD: ~70k–130k (más contexto por los findings)
- Tokens codefit (2 × incremental): ~136k input / 28k output
- Findings añadidos al contexto del orquestador: ~8k tokens adicionales
- Costo codefit: ~$0.62
- Costo SDD session: ~$0.42–$0.78 (contexto más grande)
- **Total: ~$1.04–$1.40**

**MCP cuesta aproximadamente 1.5x–2x más por sesión que CLI manual, con la ventaja de ser automático.**

---

## 5. Comparación: Cloud vs Local (Ollama)

| Dimensión | Cloud (Sonnet) | Ollama local (Qwen3:30b) |
|---|---|---|
| **Costo API** | $0.31–$1.93 por scan | $0.00 |
| **Velocidad** | Rápida (paralelo, múltiples requests) | Lenta (secuencial, 1 GPU) |
| **Tiempo full scan (Medium)** | 25–60 seg | 120–400 seg |
| **Privacidad del código** | ❌ El código sale a la API de Anthropic | ✅ El código nunca sale de la máquina |
| **Calidad de code review** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ (notablemente inferior para razonamiento profundo) |
| **Disponibilidad** | Requiere internet | ✅ Offline |
| **Rate limits** | Sí (pueden serializar requests) | No aplica |
| **Hardware requerido** | Cualquier máquina | GPU con ≥ 16GB VRAM para calidad aceptable |
| **Reproducibilidad** | Alta (mismo modelo siempre) | Variable (temperatura, versión del modelo) |
| **Setup inicial** | 2 comandos | Instalar Ollama + descargar modelo (10–30 GB) |

### ¿Cuándo usar Ollama en lugar de cloud?

- Código propietario que no puede salir de la máquina.
- Proyectos con auditorías frecuentes donde el costo API acumula.
- Entornos sin internet (air-gapped).
- Desarrollo y testing del propio codefit.

---

## 6. Comparación: Full scan vs Incremental vs --no-llm

| Modo | Tokens | Costo (Medium) | Tiempo (Medium) | Cuándo usarlo |
|---|---|---|---|---|
| `codefit scan --no-llm` | 0 | $0.00 | 3–8 seg | Pre-commit hook, CI ultra-rápido |
| `codefit scan --since HEAD~1` | ~82k | ~$0.31 | 10–22 seg | Flujo diario, cada PR |
| `codefit scan` (full) | ~379k | ~$1.93 | 25–60 seg | Release, onboarding de proyecto nuevo |
| `codefit run` (scan + bench) | ~379k + 0 | ~$1.93 + tiempo Docker | 3–8 min | Antes de releases importantes |

---

## 7. Overhead específico del modelo stateless en MCP

El modelo stateless (elegido) tiene implicaciones concretas en tokens:

**Cada tool call incluye en su input:**
- System prompt del sensor: ~500–800 tokens (siempre, no se cachea entre calls)
- Contexto del proyecto: ~200–500 tokens (paths, lenguaje, config)
- El código/schema a analizar: variable

**Sin caching entre calls**, una sesión MCP con 5 tool calls repite el system prompt 5 veces:
- Overhead fijo por call: ~700–1300 tokens de input
- 5 calls: ~3.5k–6.5k tokens de overhead que no existirían en modo stateful

**Con prompt caching de Anthropic (disponible en API):**
El system prompt puede cachearse entre calls. El overhead se reduce a ~$0.01 por sesión.
Esto convierte al stateless en prácticamente equivalente al stateful en costo, manteniendo toda la simplicidad.

**Recomendación: implementar prompt caching desde Fase 0 para el MCP server.**

---

## 8. Resumen ejecutivo de la comparación

### Cuadro de decisión rápida

| Situación | Modo recomendado | Costo est. | Tiempo est. |
|---|---|---|---|
| Pre-commit hook | `scan --no-llm --sensor security` | $0 | < 5 seg |
| Review diario de PR | `scan --since origin/main` | ~$0.31 | ~15 seg |
| Release mensual | `run` (full) | ~$1.93 | ~3–5 min |
| En sesión MCP/SDD (por archivo) | tool call específica | ~$0.05–$0.15 | ~8–15 seg |
| Proyecto con código propietario | Ollama | ~$0 | ~3–5 min |
| Onboarding proyecto existente | `scan` (full, cloud) | ~$1.93 | ~1 min |

### Los 3 trade-offs más importantes

**1. Calidad vs Costo:**
Code review con Sonnet es 8–12x más caro que con Haiku y notablemente mejor. No hay forma de tener ambos.
Solución: Sonnet para security y review, Haiku solo donde la calidad no es crítica (summary, DB inference).

**2. Automatización MCP vs Costo de sesión:**
MCP integrado en SDD cuesta 1.5x–2x más por sesión pero elimina la fricción de ejecutarlo manualmente.
Para frecuencia diaria (1 PR/día), la diferencia es ~$7/mes vs ~$15/mes. Ambos son razonables.

**3. Full scan vs Incremental:**
Full scan es 4–5x más caro y lento que incremental.
La estrategia óptima: incremental en flujo diario, full scan solo en releases.

### Lo que no cambia independientemente del modo

- Los sensores estáticos (regex, AST, schema) son siempre gratis y < 10 segundos.
- Los benchmarks de complejidad son siempre Docker, siempre lentos (no tienen alternativa).
- La calidad de los findings no depende del modo de invocación (CLI vs MCP), sino del modelo elegido.
