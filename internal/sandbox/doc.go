// Package sandbox manages the ephemeral Docker containers used by the
// complexity sensor to benchmark functions in isolation: no network, capped CPU
// and memory, read-only filesystem except /tmp, auto-removed (PRD section 17).
// Without Docker the complexity sensor is skipped; everything else still works.
//
// [NewSandbox] detects Docker; [Sandbox.Run] executes a [ContainerSpec] with
// the isolation flags enforced and returns a [ContainerResult]. Without Docker
// it returns a clear error rather than panicking, so the rest of codefit keeps
// working.
package sandbox
