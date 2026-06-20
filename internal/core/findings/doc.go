// Package findings defines the universal, language-agnostic vocabulary of an
// audit: the [Finding] hallazgo, its [Severity] and [Dimension], the
// [ConsentRecord] that authorizes suppressing a critical security finding, and
// the [SensorResult] every sensor returns.
//
// This package is a leaf: it has no dependencies on other codefit packages so
// that every layer (core, sensors, providers, cli, mcp) can import it without
// risking an import cycle.
package findings
