package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitDir(t *testing.T, project string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func names(p Project) []string {
	var out []string
	for _, s := range p.Scripts {
		out = append(out, s.Dir+":"+s.Name)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestScan(t *testing.T) {
	root := t.TempDir()

	// a plain project with .nvmrc
	plain := filepath.Join(root, "plain")
	gitDir(t, plain)
	write(t, filepath.Join(plain, "package.json"), `{"scripts":{"start":"nest start","build":"nest build"}}`)
	write(t, filepath.Join(plain, ".nvmrc"), "v12\n")

	// npm workspaces (array)
	ws := filepath.Join(root, "ws")
	gitDir(t, ws)
	write(t, filepath.Join(ws, "package.json"), `{"workspaces":["apps/*"],"scripts":{"dev":"x"}}`)
	write(t, filepath.Join(ws, "apps", "server", "package.json"), `{"scripts":{"dev":"node ."}}`)
	write(t, filepath.Join(ws, "packages", "lib", "package.json"), `{"scripts":{"build":"tsc"}}`)

	// workspaces in object form
	wsObj := filepath.Join(root, "wsobj")
	gitDir(t, wsObj)
	write(t, filepath.Join(wsObj, "package.json"), `{"workspaces":{"packages":["libs/*"]}}`)
	write(t, filepath.Join(wsObj, "libs", "a", "package.json"), `{"scripts":{"test":"jest"}}`)

	// Nx-like: no package.json under apps/, everything in the root scripts
	nx := filepath.Join(root, "nx")
	gitDir(t, nx)
	write(t, filepath.Join(nx, "package.json"), `{"scripts":{"s:api:dev1":"nx serve api","s:web:dev1":"nx serve web"}}`)
	write(t, filepath.Join(nx, "apps", "login", "project.json"), `{}`)

	// a project without git: it still appears, just with IsGit=false
	write(t, filepath.Join(root, "nogit", "package.json"), `{"scripts":{"a":"b"}}`)

	// should be skipped
	write(t, filepath.Join(root, "nopkg", "README.md"), `x`)
	gitDir(t, filepath.Join(root, ".hidden"))
	write(t, filepath.Join(root, ".hidden", "package.json"), `{}`)
	broken := filepath.Join(root, "broken")
	gitDir(t, broken)
	write(t, filepath.Join(broken, "package.json"), `{nope`)

	projects, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Project{}
	var order []string
	for _, p := range projects {
		byName[p.Name] = p
		order = append(order, p.Name)
	}
	if !equal(order, []string{"nogit", "nx", "plain", "ws", "wsobj"}) {
		t.Fatalf("projects: %v", order)
	}
	if byName["nogit"].IsGit || !byName["plain"].IsGit {
		t.Errorf("IsGit: nogit=%v plain=%v", byName["nogit"].IsGit, byName["plain"].IsGit)
	}
	if p := byName["plain"]; !p.HasNvmrc || !equal(names(p), []string{".:build", ".:start"}) {
		t.Errorf("plain: %+v", p)
	}
	if p := byName["ws"]; !equal(names(p), []string{".:dev", "apps/server:dev"}) {
		t.Errorf("ws: %v", names(p))
	}
	if p := byName["wsobj"]; !equal(names(p), []string{"libs/a:test"}) {
		t.Errorf("wsobj: %v", names(p))
	}
	if p := byName["nx"]; !equal(names(p), []string{".:s:api:dev1", ".:s:web:dev1"}) {
		t.Errorf("nx: %v", names(p))
	}
	if byName["plain"].Path != plain {
		t.Errorf("Path = %q", byName["plain"].Path)
	}
}

func TestDefaultWorkspaceDirsWithoutField(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "p")
	gitDir(t, p)
	write(t, filepath.Join(p, "package.json"), `{"scripts":{}}`)
	write(t, filepath.Join(p, "apps", "web", "package.json"), `{"scripts":{"dev":"vite"}}`)
	write(t, filepath.Join(p, "packages", "shared", "package.json"), `{"scripts":{"build":"tsc"}}`)
	proj, err := LoadProject(p)
	if err != nil {
		t.Fatal(err)
	}
	if !equal(names(proj), []string{"apps/web:dev", "packages/shared:build"}) {
		t.Fatalf("scripts: %v", names(proj))
	}
}
