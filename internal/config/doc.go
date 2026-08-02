// Package config models and loads .codefit.yaml, the per-project configuration
// committed to the repository. It is a leaf package (no codefit dependencies)
// so that both the core context and the language providers can reference its
// types without creating import cycles.
//
// Status: BUILT. The struct shape mirrors the PRD's .codefit.yaml and Load
// validates it, reporting errors located to the offending line. There is no
// global config to merge with, by design: codefit manages no models and no
// credentials, so .codefit.yaml is the only configuration there is.
package config
