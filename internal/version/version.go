// Package version computes a short hash of the running binary's content. It changes on
// every rebuild, so the TUI can notice if the daemon is running an older binary.
package version

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sync"
)

var (
	once  sync.Once
	value string
)

// Get: the binary's hash as read at the moment of the first call (unchanged after that).
func Get() string {
	once.Do(func() { value = compute() })
	return value
}

func compute() string {
	exe, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	f, err := os.Open(exe)
	if err != nil {
		return "unknown"
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}
