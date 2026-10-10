package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blixa/internal/config"
)

func TestConfigureRootFromFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	root := filepath.Join(os.Getenv("HOME"), "projects")
	os.Mkdir(root, 0o755)
	cfg := config.Default()
	var out strings.Builder
	if err := configureRoot(&cfg, cfgPath, root, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	saved, _ := config.Load(cfgPath)
	if cfg.Root != "~/projects" || saved.Root != "~/projects" || out.Len() != 0 {
		t.Fatalf("cfg=%q saved=%q out=%q", cfg.Root, saved.Root, out.String())
	}
}

func TestConfigureRootRejectsMissingDir(t *testing.T) {
	cfg := config.Default()
	err := configureRoot(&cfg, filepath.Join(t.TempDir(), "config.json"), "/nope/nope", strings.NewReader(""), &strings.Builder{})
	if err == nil {
		t.Fatal("expected an error for a nonexistent directory")
	}
}

func TestConfigureRootPromptsOnFirstRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(os.Getenv("HOME"), "code")
	os.Mkdir(root, 0o755)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	var out strings.Builder
	if err := configureRoot(&cfg, cfgPath, "", strings.NewReader("~/code\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "projects") || cfg.Root != "~/code" {
		t.Fatalf("out=%q root=%q", out.String(), cfg.Root)
	}
	// if a config already exists, it doesn't ask again
	out.Reset()
	if err := configureRoot(&cfg, cfgPath, "", strings.NewReader(""), &out); err != nil || out.Len() != 0 {
		t.Fatalf("second run: err=%v out=%q", err, out.String())
	}
}

func TestConfigureRootSwitchKeepsSelectionsPerRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	os.Mkdir(filepath.Join(home, "a"), 0o755)
	os.Mkdir(filepath.Join(home, "b"), 0o755)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	in, out := strings.NewReader(""), &strings.Builder{}
	if err := configureRoot(&cfg, cfgPath, "~/a", in, out); err != nil {
		t.Fatal(err)
	}
	cfg.Toggle("api", config.Ref{Dir: ".", Script: "start"})
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := configureRoot(&cfg, cfgPath, "~/b", in, out); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 0 {
		t.Fatalf("root b must start empty, got %+v", cfg.Projects)
	}
	if err := configureRoot(&cfg, cfgPath, "~/a", in, out); err != nil {
		t.Fatal(err)
	}
	saved, _ := config.Load(cfgPath)
	if !saved.IsSelected("api", config.Ref{Dir: ".", Script: "start"}) {
		t.Fatalf("root a's selections were lost: %+v", saved)
	}
}
