// Package pipeline defines the filtering pyramid (PRD section 15): cheap layers
// run first and only what they cannot conclude is escalated to the next, more
// expensive layer. Layer 0 (change filter), 1 (regex) and 2 (AST) are all that
// codefit runs — MCP-first, codefit never reaches a layer-3 LLM; the agent
// reasons over the mapped surface with its own model.
//
// [Pipeline] runs its [LayerProcessor] tiers in [FilterLayer] order, threading
// each layer's escalated files into the next. The concrete layer
// implementations arrive with the sensors.
//
// Status: INERT. Built and tested, but not yet wired to any consumer. The MCP
// orchestrator will use it in Fase 1; kept in the core per PRD section 15.
package pipeline
