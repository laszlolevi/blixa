package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// knownPackageManagers: the package managers blixa can run scripts and installs with.
var knownPackageManagers = map[string]bool{"npm": true, "pnpm": true, "yarn": true, "bun": true}

// lockfiles, in detection order.
var lockfiles = []struct{ file, pm string }{
	{"pnpm-lock.yaml", "pnpm"},
	{"yarn.lock", "yarn"},
	{"bun.lock", "bun"},
	{"bun.lockb", "bun"},
	{"package-lock.json", "npm"},
}

// DetectPackageManager: the package manager of the project at path — the "packageManager"
// field of package.json (corepack) first, then the lockfile; npm by default.
func DetectPackageManager(path string) string {
	var pkg struct {
		PackageManager string `json:"packageManager"`
	}
	if data, err := os.ReadFile(filepath.Join(path, "package.json")); err == nil {
		_ = json.Unmarshal(data, &pkg)
	}
	if name, _, _ := strings.Cut(pkg.PackageManager, "@"); knownPackageManagers[name] {
		return name
	}
	for _, lf := range lockfiles {
		if exists(filepath.Join(path, lf.file)) {
			return lf.pm
		}
	}
	return "npm"
}

// pnpmWorkspacePatterns: the "packages" globs of pnpm-workspace.yaml, nil if there's no such
// file. A minimal reader for this one list, so no YAML dependency is needed; negated ("!")
// patterns are skipped.
func pnpmWorkspacePatterns(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, "pnpm-workspace.yaml"))
	if err != nil {
		return nil
	}
	var patterns []string
	inPackages := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		topLevel := !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t")
		if topLevel && !strings.HasPrefix(trimmed, "-") {
			inPackages = strings.HasPrefix(trimmed, "packages:")
			continue
		}
		if !inPackages || !strings.HasPrefix(trimmed, "-") {
			continue
		}
		p := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "-")), `"'`)
		if p != "" && !strings.HasPrefix(p, "!") {
			patterns = append(patterns, p)
		}
	}
	return patterns
}
