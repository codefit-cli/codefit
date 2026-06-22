package baseline

import "github.com/codefit-cli/codefit/internal/core/findings"

// Snapshot is a recorded set of findings (identified by a stable fingerprint)
// captured at a point in time. Findings present in the snapshot are historical
// debt; findings absent from it are new.
type Snapshot struct {
	// Fingerprints holds the stable identity of each baselined finding.
	Fingerprints []string `json:"fingerprints"`
	CreatedAt    string   `json:"created_at"`
	Commit       string   `json:"commit,omitempty"`
}

// Store persists and applies a baseline.
//
// Skeleton: no implementation yet (Fase 1).
type Store interface {
	// Take snapshots the given findings as the new baseline.
	Take(fs []findings.Finding) (Snapshot, error)
	// Mark flags findings already present in the snapshot as Baselined.
	Mark(snap Snapshot, fs []findings.Finding) []findings.Finding
}
