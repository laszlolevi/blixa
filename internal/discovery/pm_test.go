package discovery

import (
	"path/filepath"
	"testing"
)

func TestDetectPackageManager(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"default", map[string]string{"package.json": `{}`}, "npm"},
		{"npm lockfile", map[string]string{"package.json": `{}`, "package-lock.json": `{}`}, "npm"},
		{"pnpm lockfile", map[string]string{"package.json": `{}`, "pnpm-lock.yaml": ``}, "pnpm"},
		{"yarn lockfile", map[string]string{"package.json": `{}`, "yarn.lock": ``}, "yarn"},
		{"bun lockfile", map[string]string{"package.json": `{}`, "bun.lock": ``}, "bun"},
		{"bun binary lockfile", map[string]string{"package.json": `{}`, "bun.lockb": ``}, "bun"},
		{"packageManager field wins", map[string]string{"package.json": `{"packageManager":"pnpm@9.1.0+sha256.abc"}`, "yarn.lock": ``}, "pnpm"},
		{"unknown packageManager falls back", map[string]string{"package.json": `{"packageManager":"weird@1"}`, "yarn.lock": ``}, "yarn"},
	}
	for _, c := range cases {
		dir := t.TempDir()
		for f, content := range c.files {
			write(t, filepath.Join(dir, f), content)
		}
		if got := DetectPackageManager(dir); got != c.want {
			t.Errorf("%s: DetectPackageManager = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPnpmWorkspaceYAML(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mono")
	write(t, filepath.Join(dir, "package.json"), `{"scripts":{"build":"turbo build"}}`)
	write(t, filepath.Join(dir, "pnpm-lock.yaml"), ``)
	write(t, filepath.Join(dir, "pnpm-workspace.yaml"), `# workspace
packages:
  - "services/*"
  - 'tools/cli'
  - '!**/test/**'
catalog:
  react: ^18
`)
	write(t, filepath.Join(dir, "services", "api", "package.json"), `{"scripts":{"dev":"tsx watch"}}`)
	write(t, filepath.Join(dir, "tools", "cli", "package.json"), `{"scripts":{"start":"node ."}}`)
	write(t, filepath.Join(dir, "apps", "web", "package.json"), `{"scripts":{"dev":"vite"}}`) // not listed: ignored
	proj, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if proj.PackageManager != "pnpm" {
		t.Errorf("PackageManager = %q", proj.PackageManager)
	}
	if !equal(names(proj), []string{".:build", "services/api:dev", "tools/cli:start"}) {
		t.Fatalf("scripts: %v", names(proj))
	}
}
