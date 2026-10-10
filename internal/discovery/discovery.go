// Package discovery finds npm projects under root and their scripts.
package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Script struct {
	Dir     string // package dir relative to the project root, "." for the root
	Name    string
	Command string
}

type Project struct {
	Name     string
	Path     string
	HasNvmrc bool
	IsGit    bool // git operations (pull, checkout, branch status) only apply to git repos
	Scripts  []Script
}

type packageJSON struct {
	Scripts    map[string]string `json:"scripts"`
	Workspaces json.RawMessage   `json:"workspaces"`
}

// Scan returns the direct subdirectories of root that contain a package.json.
func Scan(root string) ([]Project, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []Project
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(root, e.Name())
		if !exists(filepath.Join(p, "package.json")) {
			continue
		}
		proj, err := LoadProject(p)
		if err != nil {
			continue // invalid package.json: skip it
		}
		out = append(out, proj)
	}
	return out, nil
}

func LoadProject(path string) (Project, error) {
	rootPkg, err := readPackage(filepath.Join(path, "package.json"))
	if err != nil {
		return Project{}, err
	}
	proj := Project{
		Name:     filepath.Base(path),
		Path:     path,
		HasNvmrc: exists(filepath.Join(path, ".nvmrc")),
		IsGit:    exists(filepath.Join(path, ".git")),
	}
	proj.Scripts = append(proj.Scripts, scriptsOf(".", rootPkg)...)
	for _, dir := range workspaceDirs(path, rootPkg) {
		pkg, err := readPackage(filepath.Join(path, dir, "package.json"))
		if err != nil {
			continue
		}
		proj.Scripts = append(proj.Scripts, scriptsOf(dir, pkg)...)
	}
	return proj, nil
}

func readPackage(path string) (packageJSON, error) {
	var pkg packageJSON
	data, err := os.ReadFile(path)
	if err != nil {
		return pkg, err
	}
	err = json.Unmarshal(data, &pkg)
	return pkg, err
}

func scriptsOf(dir string, pkg packageJSON) []Script {
	names := make([]string, 0, len(pkg.Scripts))
	for n := range pkg.Scripts {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Script, 0, len(names))
	for _, n := range names {
		out = append(out, Script{Dir: dir, Name: n, Command: pkg.Scripts[n]})
	}
	return out
}

// workspaceDirs: the workspace directories; without a "workspaces" field, apps/* and packages/*.
func workspaceDirs(root string, pkg packageJSON) []string {
	patterns := workspacePatterns(pkg.Workspaces)
	if patterns == nil {
		patterns = []string{"apps/*", "packages/*"}
	}
	seen := map[string]bool{}
	var dirs []string
	for _, pat := range patterns {
		matches, _ := filepath.Glob(filepath.Join(root, pat, "package.json"))
		for _, m := range matches {
			rel, err := filepath.Rel(root, filepath.Dir(m))
			if err != nil || seen[rel] || strings.Contains(rel, "node_modules") {
				continue
			}
			seen[rel] = true
			dirs = append(dirs, rel)
		}
	}
	sort.Strings(dirs)
	return dirs
}

// workspacePatterns: the "workspaces" field as an array or as {"packages": [...]}.
func workspacePatterns(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var obj struct {
		Packages []string `json:"packages"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Packages
	}
	return nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
