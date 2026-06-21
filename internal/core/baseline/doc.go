// Package baseline implements the adoption baseline (PRD RF-08): a committed
// snapshot of a project's findings so that, with baseline enabled, codefit
// reports only new findings while pre-existing debt is recorded (baselined:
// true) and does not block. This makes adopting codefit on an existing project
// painless (Scenario B).
//
// Status: SKELETON. This declares the [Snapshot] type and the [Store] contract.
// The snapshot/diff logic and the on-disk format (.codefit/baseline.json) are
// implemented in Fase 1.
package baseline
