package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/codefit-cli/codefit/internal/core/findings"
)

// Cache stores findings keyed by a file's content hash under Dir (default
// .codefit/cache). A cache hit means the file content is unchanged, so its
// findings can be reused instead of recomputed (PRD §15, optimization 2).
type Cache struct {
	Dir string
}

// HashFile returns the hex-encoded SHA-256 of a file's content.
func (c *Cache) HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening %q for hashing: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hashing %q: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Get returns the cached findings for a content hash and whether they were
// present. A missing or unreadable entry is treated as a miss.
func (c *Cache) Get(fileHash string) ([]findings.Finding, bool) {
	data, err := os.ReadFile(c.entryPath(fileHash))
	if err != nil {
		return nil, false
	}
	var fs []findings.Finding
	if err := json.Unmarshal(data, &fs); err != nil {
		return nil, false
	}
	return fs, true
}

// Set stores the findings computed for a content hash as a JSON file.
func (c *Cache) Set(fileHash string, fs []findings.Finding) error {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}
	data, err := json.Marshal(fs)
	if err != nil {
		return fmt.Errorf("encoding cache entry: %w", err)
	}
	if err := os.WriteFile(c.entryPath(fileHash), data, 0o644); err != nil {
		return fmt.Errorf("writing cache entry: %w", err)
	}
	return nil
}

func (c *Cache) entryPath(fileHash string) string {
	return filepath.Join(c.Dir, fileHash+".json")
}
