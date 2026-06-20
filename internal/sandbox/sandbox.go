package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

const (
	defaultMemoryMB = 512
	defaultCPUs     = 1.0
)

// Sandbox runs ephemeral Docker containers for the complexity sensor: no
// network, capped CPU/memory, read-only filesystem, auto-removed (PRD §17).
type Sandbox struct {
	DockerAvailable bool
}

// ContainerSpec describes a single container run.
type ContainerSpec struct {
	Image      string
	Command    []string
	BindMounts []string // mounted read-only
	TimeoutSec int
	MemoryMB   int     // default 512
	CPUs       float64 // default 1.0
}

// ContainerResult is the captured output of a container run.
type ContainerResult struct {
	Stdout     string
	Stderr     string
	ExitCode   int
	DurationMs int64
}

// NewSandbox detects whether Docker is available on PATH. It never errors on
// absence — the caller decides how to degrade (the complexity sensor is skipped
// with a warning).
func NewSandbox() (*Sandbox, error) {
	_, err := exec.LookPath("docker")
	return &Sandbox{DockerAvailable: err == nil}, nil
}

// IsAvailable reports whether Docker was detected.
func (s *Sandbox) IsAvailable() bool { return s.DockerAvailable }

// Run executes a single container according to spec. When Docker is unavailable
// it returns a clear error rather than panicking. A non-zero container exit is
// reported via ContainerResult.ExitCode, not as an error; only failures to run
// the container (missing binary, timeout) are returned as errors.
func (s *Sandbox) Run(ctx context.Context, spec ContainerSpec) (ContainerResult, error) {
	if !s.DockerAvailable {
		return ContainerResult{}, errors.New("docker is not available; the complexity sensor requires Docker")
	}

	if spec.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(spec.TimeoutSec)*time.Second)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "docker", buildDockerArgs(spec)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	res := ContainerResult{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMs: time.Since(start).Milliseconds(),
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			return res, nil
		}
		return res, fmt.Errorf("running container: %w", err)
	}
	return res, nil
}

// buildDockerArgs assembles the `docker run` arguments enforcing the isolation
// and resource limits.
func buildDockerArgs(spec ContainerSpec) []string {
	mem := spec.MemoryMB
	if mem <= 0 {
		mem = defaultMemoryMB
	}
	cpus := spec.CPUs
	if cpus <= 0 {
		cpus = defaultCPUs
	}

	args := []string{
		"run", "--rm",
		"--network", "none",
		"--read-only",
		"--memory", fmt.Sprintf("%dm", mem),
		"--cpus", strconv.FormatFloat(cpus, 'g', -1, 64),
	}
	for _, m := range spec.BindMounts {
		args = append(args, "-v", fmt.Sprintf("%s:%s:ro", m, m))
	}
	args = append(args, spec.Image)
	args = append(args, spec.Command...)
	return args
}
