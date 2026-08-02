// Package sensors defines the [Sensor] contract: a module that audits one
// [findings.Dimension] and returns a [findings.SensorResult]. Sensors live in
// the core and are language-agnostic — they orchestrate the filtering pyramid
// and ask the active LanguageProvider for the concrete, language-specific data
// (queries, rules, schema parsing).
//
// This package declares only the interface. The concrete sensors live in
// subpackages: security and db are built (sensors/security, sensors/db); review,
// complexity and tests arrive in later phases.
package sensors
