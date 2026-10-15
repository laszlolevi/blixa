// Package version computes a short hash of the running binary's content. It changes on
// every rebuild, so the TUI can notice if the daemon is running an older binary. Tag is
// the human-readable git tag shown to the user.
package version

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
)

var (
	once  sync.Once
	value string
)

// tag is set by the release build: -ldflags "-X github.com/laszlolevi/blixa/internal/version.tag=v1.2.3".
var tag string

// pseudoVersion matches the timestamp-revision part of a Go pseudo-version, which go build
// stamps when the commit isn't tagged (e.g. v0.2.1-0.20261011103554-78b80034ed03).
var pseudoVersion = regexp.MustCompile(`[-.]\d{14}-[0-9a-f]{12}(\+|$)`)

// Tag: the release tag; for a local build the one go build stamped from git ("+dirty" with
// uncommitted changes), or "dev <commit>" if the commit isn't tagged.
func Tag() string {
	if tag != "" {
		return tag
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	var rev string
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			rev = s.Value
		}
	}
	return tagFrom(info.Main.Version, rev)
}

func tagFrom(version, rev string) string {
	if version == "" || version == "(devel)" {
		return "dev"
	}
	if !pseudoVersion.MatchString(version) {
		return version
	}
	s := "dev"
	if len(rev) >= 7 {
		s += " " + rev[:7]
	}
	if strings.HasSuffix(version, "+dirty") {
		s += "+dirty"
	}
	return s
}

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
