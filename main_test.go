package main

import (
	"errors"
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

func TestConfigureRootSuggestsWorkingDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wd := filepath.Join(os.Getenv("HOME"), "dev")
	os.Mkdir(wd, 0o755)
	t.Chdir(wd)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	var out strings.Builder
	if err := configureRoot(&cfg, cfgPath, "", strings.NewReader("\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[~/dev]") || cfg.Root != "~/dev" {
		t.Fatalf("out=%q root=%q", out.String(), cfg.Root)
	}
}

func TestConfigureRootSuggestsParentOfProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wd := filepath.Join(os.Getenv("HOME"), "dev", "api")
	os.MkdirAll(wd, 0o755)
	os.WriteFile(filepath.Join(wd, "package.json"), []byte("{}"), 0o644)
	t.Chdir(wd)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	var out strings.Builder
	if err := configureRoot(&cfg, cfgPath, "", strings.NewReader("\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[~/dev]") || cfg.Root != "~/dev" {
		t.Fatalf("out=%q root=%q", out.String(), cfg.Root)
	}
}

// fakeUpdate records what maybeUpdate did.
type fakeUpdate struct {
	applied, execed string
	applyErr        error
}

func (f *fakeUpdate) deps(running bool, current, latest string, latestErr error) updateDeps {
	return updateDeps{
		DaemonRunning: func() bool { return running },
		Current:       current,
		Latest:        func() (string, error) { return latest, latestErr },
		Apply: func(tag string) error {
			f.applied = tag
			return f.applyErr
		},
		Exec: func() error {
			f.execed = "yes"
			return nil
		},
	}
}

func TestMaybeUpdateSkips(t *testing.T) {
	cases := []struct {
		name            string
		running         bool
		current, latest string
		latestErr       error
	}{
		{"daemon running", true, "v0.2.0", "v0.3.0", nil},
		{"dev build", false, "dev 78b8003", "v0.3.0", nil},
		{"dirty build", false, "v0.2.0+dirty", "v0.3.0", nil},
		{"check failed", false, "v0.2.0", "", errors.New("offline")},
		{"up to date", false, "v0.3.0", "v0.3.0", nil},
	}
	for _, c := range cases {
		f := &fakeUpdate{}
		var out strings.Builder
		if err := maybeUpdate(f.deps(c.running, c.current, c.latest, c.latestErr), strings.NewReader("\n"), &out); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if out.Len() != 0 || f.applied != "" || f.execed != "" {
			t.Fatalf("%s: out=%q applied=%q execed=%q", c.name, out.String(), f.applied, f.execed)
		}
	}
}

func TestMaybeUpdateDeclined(t *testing.T) {
	f := &fakeUpdate{}
	var out strings.Builder
	if err := maybeUpdate(f.deps(false, "v0.2.0", "v0.3.0", nil), strings.NewReader("n\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "v0.3.0 is available (you have v0.2.0)") || f.applied != "" || f.execed != "" {
		t.Fatalf("out=%q applied=%q execed=%q", out.String(), f.applied, f.execed)
	}
}

func TestMaybeUpdateAccepted(t *testing.T) {
	f := &fakeUpdate{}
	var out strings.Builder
	if err := maybeUpdate(f.deps(false, "v0.2.0", "v0.3.0", nil), strings.NewReader("\n"), &out); err != nil {
		t.Fatal(err)
	}
	if f.applied != "v0.3.0" || f.execed != "yes" || !strings.Contains(out.String(), "updated to v0.3.0") {
		t.Fatalf("out=%q applied=%q execed=%q", out.String(), f.applied, f.execed)
	}
}

func TestMaybeUpdateApplyFails(t *testing.T) {
	f := &fakeUpdate{applyErr: errors.New("permission denied")}
	var out strings.Builder
	if err := maybeUpdate(f.deps(false, "v0.2.0", "v0.3.0", nil), strings.NewReader("y\n"), &out); err != nil {
		t.Fatal(err)
	}
	if f.execed != "" || !strings.Contains(out.String(), "update failed: permission denied") {
		t.Fatalf("out=%q execed=%q", out.String(), f.execed)
	}
}
