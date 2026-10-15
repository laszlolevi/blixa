package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingReturnsDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Root != "~/workspace" || cfg.Projects == nil || len(cfg.Projects) != 0 {
		t.Fatalf("unexpected default: %+v", cfg)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	cfg := Default()
	cfg.Toggle("my-api", Ref{Dir: ".", Script: "s:api:dev1"})
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsSelected("my-api", Ref{Dir: ".", Script: "s:api:dev1"}) {
		t.Fatalf("selection was lost: %+v", got)
	}
}

func TestLoadSyntaxErrorHasPositionAndKeepsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := "{\n  \"root\": ,\n}"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), ":2:11:") {
		t.Fatalf("expected a positional error (:2:11:), got: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != content {
		t.Fatal("the invalid file was modified")
	}
}

func TestParseRef(t *testing.T) {
	r, err := ParseRef(".:s:api:dev1")
	if err != nil || r.Dir != "." || r.Script != "s:api:dev1" {
		t.Fatalf("ParseRef = %+v, %v", r, err)
	}
	if r.String() != ".:s:api:dev1" {
		t.Fatalf("String() = %q", r.String())
	}
	for _, bad := range []string{"nocolon", ":x", "x:"} {
		if _, err := ParseRef(bad); err == nil {
			t.Errorf("ParseRef(%q) should have returned an error", bad)
		}
	}
}

func TestToggleAndRemove(t *testing.T) {
	cfg := Default()
	r := Ref{Dir: "apps/server", Script: "dev"}
	cfg.Toggle("p", r)
	if !cfg.IsSelected("p", r) || len(cfg.Selected("p")) != 1 {
		t.Fatal("Toggle did not add it")
	}
	cfg.Toggle("p", r)
	if cfg.IsSelected("p", r) {
		t.Fatal("Toggle did not remove it")
	}
	if _, ok := cfg.Projects["p"]; ok {
		t.Fatal("an empty project remained in the config")
	}
	cfg.Toggle("q", r)
	cfg.RemoveProject("q")
	if _, ok := cfg.Projects["q"]; ok {
		t.Fatal("RemoveProject did not delete it")
	}
}

func TestSelectionsAreKeptPerRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.Toggle("api", Ref{Dir: ".", Script: "start"})
	cfg.SetRoot("~/projects")
	if len(cfg.Projects) != 0 {
		t.Fatalf("a new root must start with no projects, got %+v", cfg.Projects)
	}
	cfg.Toggle("web", Ref{Dir: ".", Script: "dev"})
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != "~/projects" || !got.IsSelected("web", Ref{Dir: ".", Script: "dev"}) || got.IsSelected("api", Ref{Dir: ".", Script: "start"}) {
		t.Fatalf("current root after load: %+v", got)
	}
	got.SetRoot("~/workspace")
	if !got.IsSelected("api", Ref{Dir: ".", Script: "start"}) || got.IsSelected("web", Ref{Dir: ".", Script: "dev"}) {
		t.Fatalf("switching back must restore the old root's selections: %+v", got.Projects)
	}
	data, _ := os.ReadFile(path)
	var raw struct {
		Projects map[string]any `json:"projects"`
		Roots    map[string]any `json:"roots"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Projects != nil || raw.Roots["~/workspace"] == nil || raw.Roots["~/projects"] == nil {
		t.Fatalf("selections must be stored per root under roots:\n%s", data)
	}
}

func TestLegacyTopLevelProjectsMigrateToCurrentRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := `{"root": "~/workspace", "projects": {"api": {"scripts": [".:start"]}}}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IsSelected("api", Ref{Dir: ".", Script: "start"}) {
		t.Fatalf("legacy selections lost: %+v", cfg)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["projects"]; ok {
		t.Fatalf("the legacy top-level projects key must not be written back:\n%s", data)
	}
	if _, ok := raw["roots"].(map[string]any)["~/workspace"]; !ok {
		t.Fatalf("selections must be stored under roots[~/workspace]:\n%s", data)
	}
}

func TestKnownRootsKeepsEmptyRootsAndRemoveRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.SetRoot("~/oss") // no selections under ~/workspace or ~/oss
	cfg.SetRoot("~/work")
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(loaded.KnownRoots()); got != "[~/oss ~/work ~/workspace]" {
		t.Fatalf("KnownRoots = %s", got)
	}
	loaded.RemoveRoot("~/oss")
	loaded.RemoveRoot("~/work") // the current root stays
	if got := fmt.Sprint(loaded.KnownRoots()); got != "[~/work ~/workspace]" {
		t.Fatalf("after RemoveRoot: %s", got)
	}
}
