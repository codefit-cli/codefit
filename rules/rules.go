// Package rules embeds codefit's declarative detection rules so they ship inside
// the single binary, versioned with it. It holds NO logic: ruleengine.LoadFS
// reads and validates the YAML, the provider compiles it — this package only
// carries the bytes. Contributors add rules as YAML here without writing Go.
package rules

import "embed"

// FS is the embedded rule tree, organised as <language>/<dimension>/*.yaml.
//
//go:embed typescript/security/*.yaml
var FS embed.FS
