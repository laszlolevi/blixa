package paths

import "testing"

func TestExpand(t *testing.T) {
	t.Setenv("HOME", "/home/x")
	cases := map[string]string{
		"~":           "/home/x",
		"~/workspace": "/home/x/workspace",
		"/abs":        "/abs",
		"rel":         "rel",
	}
	for in, want := range cases {
		if got := Expand(in); got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDirsHonourEnv(t *testing.T) {
	t.Setenv("BLIXA_CONFIG_DIR", "/c")
	t.Setenv("BLIXA_STATE_DIR", "/s")
	if got := ConfigFile(); got != "/c/config.json" {
		t.Errorf("ConfigFile() = %q", got)
	}
	if got := SocketPath(); got != "/s/blixa.sock" {
		t.Errorf("SocketPath() = %q", got)
	}
}

func TestDefaultDirs(t *testing.T) {
	t.Setenv("HOME", "/home/x")
	t.Setenv("BLIXA_CONFIG_DIR", "")
	t.Setenv("BLIXA_STATE_DIR", "")
	if got := ConfigDir(); got != "/home/x/.config/blixa" {
		t.Errorf("ConfigDir() = %q", got)
	}
	if got := StateDir(); got != "/home/x/.local/state/blixa" {
		t.Errorf("StateDir() = %q", got)
	}
}

func TestCollapse(t *testing.T) {
	t.Setenv("HOME", "/home/x")
	cases := map[string]string{
		"/home/x":           "~",
		"/home/x/workspace": "~/workspace",
		"/home/xy":          "/home/xy",
		"/opt/p":            "/opt/p",
	}
	for in, want := range cases {
		if got := Collapse(in); got != want {
			t.Errorf("Collapse(%q) = %q, want %q", in, got, want)
		}
	}
}
