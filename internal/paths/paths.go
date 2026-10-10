// Package paths provides blixa's config and state directories.
package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// ConfigDir: $BLIXA_CONFIG_DIR, defaulting to ~/.config/blixa.
func ConfigDir() string {
	if d := os.Getenv("BLIXA_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(home(), ".config", "blixa")
}

// StateDir: $BLIXA_STATE_DIR, defaulting to ~/.local/state/blixa.
func StateDir() string {
	if d := os.Getenv("BLIXA_STATE_DIR"); d != "" {
		return d
	}
	return filepath.Join(home(), ".local", "state", "blixa")
}

func ConfigFile() string { return filepath.Join(ConfigDir(), "config.json") }

func SocketPath() string { return filepath.Join(StateDir(), "blixa.sock") }

// Expand replaces a leading ~ with the home directory.
func Expand(p string) string {
	if p == "~" {
		return home()
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home(), p[2:])
	}
	return p
}

// Collapse replaces the home directory prefix with ~ (the inverse of Expand).
func Collapse(p string) string {
	h := home()
	if p == h {
		return "~"
	}
	if strings.HasPrefix(p, h+string(filepath.Separator)) {
		return "~" + p[len(h):]
	}
	return p
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}
