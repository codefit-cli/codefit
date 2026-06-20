package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanNotices(t *testing.T) {
	// No flags set: no notices.
	if n := scanNotices(""); len(n) != 0 {
		t.Errorf("no flags should yield no notices, got %v", n)
	}
	// --since set: a clear notice that incremental filtering isn't active yet.
	n := scanNotices("origin/main")
	if len(n) == 0 {
		t.Fatal("--since should produce a notice")
	}
	joined := strings.ToLower(strings.Join(n, " "))
	if !strings.Contains(joined, "since") || !strings.Contains(joined, "full") {
		t.Errorf("notice should warn that --since runs a full scan, got: %v", n)
	}
}

// TestScanHonorsOutFile runs scan end-to-end over a tiny temp project with
// --out-file set and asserts the rendered report lands in the file.
func TestScanHonorsOutFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".codefit.yaml"),
		[]byte("version: \"1\"\nproject:\n  name: \"t\"\n  language: \"go\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"),
		[]byte("package main\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outFile := filepath.Join(root, "out.md")

	// Drive scan through the real command tree with the global flags set.
	root0 := newRootCmd()
	root0.SetArgs([]string{
		"--config", filepath.Join(root, ".codefit.yaml"),
		"--out-file", outFile,
		"scan", root,
	})
	if err := root0.Execute(); err != nil {
		t.Fatalf("scan execute: %v", err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("--out-file was not written: %v", err)
	}
	if !strings.Contains(string(data), "SCORE GLOBAL") {
		t.Errorf("out-file should contain the rendered report, got:\n%s", data)
	}
	_ = os.Remove("codefit-report.json") // scan writes the canonical JSON in cwd
}
