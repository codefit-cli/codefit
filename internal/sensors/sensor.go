package sensors

import (
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
)

// Sensor audits a single dimension of a project. Every sensor maps to exactly
// one [findings.Dimension] and is driven by the orchestrator with a shared
// [auditctx.AuditContext].
//
// Skeleton: no implementation yet.
type Sensor interface {
	// Name is the sensor's stable identifier (e.g. "security").
	Name() string
	// Dimension is the audit axis this sensor produces findings for.
	Dimension() findings.Dimension
	// Run executes the sensor against the audit context and returns its result.
	Run(ctx auditctx.AuditContext) (findings.SensorResult, error)
}
