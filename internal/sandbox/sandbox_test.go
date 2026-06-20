package sandbox

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestBuildDockerArgsDefaults(t *testing.T) {
	args := buildDockerArgs(ContainerSpec{
		Image:      "alpine:3",
		Command:    []string{"echo", "hi"},
		BindMounts: []string{"/work"},
	})
	joined := strings.Join(args, " ")
	for _, want := range []string{"run", "--rm", "--network none", "--read-only", "--memory 512m", "--cpus 1", "-v /work:/work:ro"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q:\n%s", want, joined)
		}
	}
	// Image precedes the command, both at the end.
	if i := slices.Index(args, "alpine:3"); i == -1 || i != len(args)-3 {
		t.Errorf("image should precede the command at the end: %v", args)
	}
}

func TestBuildDockerArgsCustomLimits(t *testing.T) {
	args := buildDockerArgs(ContainerSpec{Image: "x", MemoryMB: 256, CPUs: 2})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--memory 256m") || !strings.Contains(joined, "--cpus 2") {
		t.Errorf("custom limits not applied: %s", joined)
	}
}

func TestRunWhenUnavailableErrorsNoPanic(t *testing.T) {
	s := &Sandbox{DockerAvailable: false}
	_, err := s.Run(context.Background(), ContainerSpec{Image: "alpine"})
	if err == nil {
		t.Fatal("Run without Docker should return a clear error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "docker") {
		t.Errorf("error should mention docker: %v", err)
	}
}

func TestIsAvailableReflectsField(t *testing.T) {
	if (&Sandbox{DockerAvailable: true}).IsAvailable() != true {
		t.Error("IsAvailable should mirror DockerAvailable")
	}
	if (&Sandbox{DockerAvailable: false}).IsAvailable() != false {
		t.Error("IsAvailable should mirror DockerAvailable")
	}
}

func TestNewSandbox(t *testing.T) {
	s, err := NewSandbox()
	if err != nil {
		t.Fatalf("NewSandbox errored: %v", err)
	}
	if s == nil {
		t.Fatal("NewSandbox returned nil")
	}
	// IsAvailable must equal the detected field (no panic either way).
	_ = s.IsAvailable()
}
