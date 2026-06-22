package cli

import "os"

// fileExists reports whether path exists and is readable.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
