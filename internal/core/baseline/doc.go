// Package baseline implements the adoption baseline (PRD RF-08): a committed
// snapshot of a project's findings so that, with baseline enabled, codefit
// reports only new findings while pre-existing debt is recorded (baselined:
// true) and does not block. This makes adopting codefit on an existing project
// painless (Scenario B).
//
// Status: BUILT (Fase 1). The snapshot/diff logic and the on-disk format are
// implemented. The file is named by [Name] — ".codefit-baseline" at the repo
// root, a committed plain file, NOT ".codefit/baseline.json" as an earlier draft
// of this comment claimed.
package baseline
