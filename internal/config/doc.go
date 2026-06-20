// Package config models and loads .codefit.yaml, the per-project configuration
// committed to the repository. It is a leaf package (no codefit dependencies)
// so that both the core context and the language providers can reference its
// types without creating import cycles.
//
// Skeleton: the struct shape mirrors the PRD's .codefit.yaml. Validation,
// defaulting and merging with the global config are not implemented yet.
package config
