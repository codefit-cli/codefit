package cache_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/cache"
	"github.com/codefit-cli/codefit/internal/core/findings"
)

func TestHashFileIsStableSHA256(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	if err := os.WriteFile(p, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &cache.Cache{Dir: filepath.Join(dir, "cache")}
	h1, err := c.HashFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(h1) != 64 {
		t.Errorf("hash length = %d, want 64 hex chars", len(h1))
	}
	h2, _ := c.HashFile(p)
	if h1 != h2 {
		t.Errorf("hash not stable: %s vs %s", h1, h2)
	}
	// Different content -> different hash.
	if err := os.WriteFile(p, []byte("package other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h3, _ := c.HashFile(p)
	if h3 == h1 {
		t.Error("hash should change when content changes")
	}
}

func TestCacheSetGetRoundTrip(t *testing.T) {
	c := &cache.Cache{Dir: filepath.Join(t.TempDir(), "cache")}
	want := []findings.Finding{
		{ID: "SEC-001", Dimension: findings.DimensionSecurity, Severity: findings.SeverityCritical, Title: "x"},
	}
	if err := c.Set("abc123", want); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok := c.Get("abc123")
	if !ok {
		t.Fatal("Get returned not-found after Set")
	}
	if len(got) != 1 || got[0].ID != "SEC-001" {
		t.Errorf("Get = %+v, want the stored finding", got)
	}
}

func TestCacheGetMiss(t *testing.T) {
	c := &cache.Cache{Dir: filepath.Join(t.TempDir(), "cache")}
	if _, ok := c.Get("nonexistent"); ok {
		t.Error("Get on a missing hash should return ok=false")
	}
}

func TestHashFileMissing(t *testing.T) {
	c := &cache.Cache{Dir: t.TempDir()}
	if _, err := c.HashFile(filepath.Join(t.TempDir(), "nope.go")); err == nil {
		t.Error("HashFile on a missing file should error")
	}
}

func TestCacheGetCorruptEntry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A non-JSON entry must be treated as a miss, not a crash.
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &cache.Cache{Dir: dir}
	if _, ok := c.Get("bad"); ok {
		t.Error("Get on a corrupt entry should return ok=false")
	}
}
