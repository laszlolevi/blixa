// Package config manages blixa's user config (config.json).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Project struct {
	Scripts []string `json:"scripts"`
}

// RootConfig holds the selections made under one root directory.
type RootConfig struct {
	Projects map[string]Project `json:"projects"`
}

// Config keeps the selected scripts separately for every root, so switching roots
// neither mixes selections nor loses them. In memory, Projects is the current root's
// selection; on disk it lives under roots[root] (a top-level "projects" key is only
// read, for configs written before roots existed).
type Config struct {
	Root     string                `json:"root"`
	Roots    map[string]RootConfig `json:"roots,omitempty"`
	Projects map[string]Project    `json:"projects,omitempty"`
}

// SetRoot switches to another root: the current selections are kept under the old root
// and the new root's selections (if any) become current.
func (c *Config) SetRoot(root string) {
	c.storeCurrent()
	c.Root = root
	c.Projects = map[string]Project{}
	if rc, ok := c.Roots[root]; ok && rc.Projects != nil {
		c.Projects = rc.Projects
	}
}

// storeCurrent writes the current selections back under roots[root]. A root without
// selections is kept too: every root in Roots is a tab in the TUI.
func (c *Config) storeCurrent() {
	if c.Roots == nil {
		c.Roots = map[string]RootConfig{}
	}
	c.Roots[c.Root] = RootConfig{Projects: c.Projects}
}

// KnownRoots: every root used so far (the current one included), sorted.
func (c Config) KnownRoots() []string {
	roots := []string{c.Root}
	for r := range c.Roots {
		if r != c.Root {
			roots = append(roots, r)
		}
	}
	sort.Strings(roots)
	return roots
}

// RemoveRoot forgets a root that isn't the current one, together with its selections.
func (c *Config) RemoveRoot(root string) {
	if root != c.Root {
		delete(c.Roots, root)
	}
}

// Ref is a script reference: "<package-dir>:<script-name>", the root dir being ".".
type Ref struct {
	Dir    string
	Script string
}

func (r Ref) String() string { return r.Dir + ":" + r.Script }

// ParseRef splits on the first ':', so the script name may still contain ':'.
func ParseRef(s string) (Ref, error) {
	i := strings.Index(s, ":")
	if i <= 0 || i == len(s)-1 {
		return Ref{}, fmt.Errorf("invalid script reference: %q", s)
	}
	return Ref{Dir: s[:i], Script: s[i+1:]}, nil
}

func Default() Config {
	return Config{Root: "~/workspace", Projects: map[string]Project{}}
}

// Load: Default if the file is missing; a line:column positional error on invalid JSON.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		var se *json.SyntaxError
		if errors.As(err, &se) {
			line, col := position(data, se.Offset)
			return Config{}, fmt.Errorf("%s:%d:%d: %v", path, line, col, err)
		}
		return Config{}, fmt.Errorf("%s: %v", path, err)
	}
	legacy := cfg.Projects
	if _, ok := cfg.Roots[cfg.Root]; !ok && len(legacy) > 0 {
		cfg.Projects = legacy // migrate the pre-roots format to the current root
	} else {
		cfg.Projects = map[string]Project{}
		if rc, ok := cfg.Roots[cfg.Root]; ok && rc.Projects != nil {
			cfg.Projects = rc.Projects
		}
	}
	return cfg, nil
}

// position: the line and column of the bad byte (offset-1), 1-indexed.
func position(data []byte, offset int64) (line, col int) {
	line, col = 1, 1
	for i := int64(0); i < offset-1 && i < int64(len(data)); i++ {
		if data[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

// Save writes the config atomically (temp file + rename).
func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	roots := make(map[string]RootConfig, len(cfg.Roots)+1)
	for k, v := range cfg.Roots {
		roots[k] = v
	}
	cfg.Roots = roots
	cfg.storeCurrent()
	cfg.Projects = nil
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Selected: the project's selected scripts; invalid entries are skipped.
func (c *Config) Selected(project string) []Ref {
	var refs []Ref
	for _, s := range c.Projects[project].Scripts {
		if r, err := ParseRef(s); err == nil {
			refs = append(refs, r)
		}
	}
	return refs
}

func (c *Config) IsSelected(project string, r Ref) bool {
	for _, s := range c.Projects[project].Scripts {
		if s == r.String() {
			return true
		}
	}
	return false
}

// Toggle adds or removes the script; a project left empty is deleted.
func (c *Config) Toggle(project string, r Ref) {
	if c.Projects == nil {
		c.Projects = map[string]Project{}
	}
	p := c.Projects[project]
	var kept []string
	found := false
	for _, s := range p.Scripts {
		if s == r.String() {
			found = true
			continue
		}
		kept = append(kept, s)
	}
	if !found {
		kept = append(kept, r.String())
	}
	if len(kept) == 0 {
		delete(c.Projects, project)
		return
	}
	p.Scripts = kept
	c.Projects[project] = p
}

func (c *Config) RemoveProject(name string) { delete(c.Projects, name) }
