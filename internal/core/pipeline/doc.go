// Package pipeline defines the filtering pyramid (PRD section 15): cheap layers
// run first and only what they cannot conclude is passed up to the next, more
// expensive layer. Layer 0 (change filter) and 1 (regex) and 2 (AST) are free;
// layer 3 (LLM) only ever sees the suspicious fragments the AST could not rule
// on.
//
// [Pipeline] runs its [LayerProcessor] tiers in [FilterLayer] order, threading
// each layer's escalated files into the next. The concrete layer
// implementations arrive with the sensors.
//
// Status: INERT. Built and tested, but not yet wired to any consumer. The MCP
// orchestrator will use it in Fase 1; kept in the core per PRD section 15.
// (The LLM layer is dropped in a later step — MCP-first: codefit never reaches
// layer 3; the agent reasons over the mapped surface.)
package pipeline
