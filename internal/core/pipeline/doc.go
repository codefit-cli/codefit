// Package pipeline defines the filtering pyramid (PRD section 15): cheap layers
// run first and only what they cannot conclude is passed up to the next, more
// expensive layer. Layer 0 (change filter) and 1 (regex) and 2 (AST) are free;
// layer 3 (LLM) only ever sees the suspicious fragments the AST could not rule
// on.
//
// [Pipeline] runs its [LayerProcessor] tiers in [FilterLayer] order, threading
// each layer's escalated files into the next and early-exiting before the LLM
// layer when the accumulated findings already satisfy --fail-on. The concrete
// layer implementations arrive with the sensors.
package pipeline
