package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sync"
)

// executable resolves the path of the running analyzer binary. It is a package
// variable, and the ONLY reason is that os.Executable's failure branch cannot be
// reached from a test on the platforms codefit ships to — the "identity
// unavailable" behaviour is a correctness requirement (R2), so it is exercised
// rather than merely asserted about. Production never reassigns it; only the
// package's own tests do, through export_test.go.
var executable = os.Executable

var (
	identityOnce sync.Once
	identityID   string
	identityErr  error
)

// Identity is the running analyzer's identity: the hex SHA-256 of the executable
// codefit is running as, computed once per process and memoized.
//
// The analysis of a file is a pure function of (file bytes, analyzer), so a cache
// key must name both. The obvious alternative — internal/version.Version — does
// not work and fails exactly where it matters most: it is the constant
// "v0.1.0-dev" for any plain go build, go run or go test, so during development,
// where the rules change several times an hour, every build would present the
// same key and the rule author would be the first person a stale entry bit.
//
// Hashing the binary covers EVERY input that can change a verdict — the YAML
// rules, the Go-coded detectors, the provider's parser, the surface queries —
// because all of them are in the binary. Under go run and go test the executable
// is a fresh temporary build, so editing a rule changes the identity
// automatically. Two builds of identical source produce different binaries and
// therefore a miss: wasted work, never a stale verdict, which is the safe
// direction.
//
// The cost is one SHA-256 of a ~5 MB binary, once. Against a walk that parses
// hundreds of files it does not register.
//
// An error means the identity is UNKNOWN, and an unknown key input means do not
// reuse: callers disable caching for the run rather than guess.
func Identity() (string, error) {
	identityOnce.Do(func() {
		identityID, identityErr = hashExecutable()
	})
	return identityID, identityErr
}

func hashExecutable() (string, error) {
	path, err := executable()
	if err != nil {
		return "", fmt.Errorf("locating the running executable: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening the running executable %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing the running executable %q: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
