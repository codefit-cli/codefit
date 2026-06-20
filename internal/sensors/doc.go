// Package sensors defines the [Sensor] contract: a module that audits one
// [findings.Dimension] and returns a [findings.SensorResult]. Sensors live in
// the core and are language-agnostic — they orchestrate the filtering pyramid
// and ask the active LanguageProvider for the concrete, language-specific data
// (queries, rules, schema parsing).
//
// Skeleton: only the interface is declared; concrete sensors (security, review,
// db, ...) arrive in later phases.
package sensors
