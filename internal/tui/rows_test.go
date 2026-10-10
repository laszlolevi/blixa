package tui

import (
	"testing"

	"blixa/internal/config"
	"blixa/internal/discovery"
)

func sampleProjects() []discovery.Project {
	return []discovery.Project{
		{Name: "alpha", Path: "/w/alpha", IsGit: true, Scripts: []discovery.Script{{Dir: ".", Name: "start"}, {Dir: ".", Name: "build"}, {Dir: "apps/web", Name: "dev"}}},
		{Name: "beta", Path: "/w/beta", IsGit: true, Scripts: []discovery.Script{{Dir: ".", Name: "start"}}},
	}
}

func sampleConfig() config.Config {
	cfg := config.Default()
	cfg.Projects["alpha"] = config.Project{Scripts: []string{".:start", "apps/web:dev"}}
	return cfg
}

func labels(rows []row) []string {
	var out []string
	for _, r := range rows {
		if r.kind == rowProject {
			out = append(out, "P:"+r.project)
		} else {
			out = append(out, "T:"+r.spec.Label())
		}
	}
	return out
}

func eq(a, b []string) bool {
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

func TestBuildRows(t *testing.T) {
	ps, cfg := sampleProjects(), sampleConfig()
	got := labels(buildRows(ps, true, cfg, map[string]bool{}, false, ""))
	if !eq(got, []string{"P:alpha", "T:start", "T:apps/web › dev"}) {
		t.Errorf("base: %v", got)
	}
	got = labels(buildRows(ps, true, cfg, map[string]bool{}, true, ""))
	if !eq(got, []string{"P:alpha", "T:start", "T:apps/web › dev", "P:beta"}) {
		t.Errorf("all: %v", got)
	}
	got = labels(buildRows(ps, true, cfg, map[string]bool{"alpha": true}, false, ""))
	if !eq(got, []string{"P:alpha"}) {
		t.Errorf("collapsed: %v", got)
	}
	got = labels(buildRows(ps, true, cfg, map[string]bool{"alpha": true}, false, "WEB"))
	if !eq(got, []string{"P:alpha", "T:apps/web › dev"}) {
		t.Errorf("filtered: %v", got)
	}
	r := buildRows(ps, true, cfg, map[string]bool{}, false, "")[1]
	if r.spec.ProjectPath != "/w/alpha" || r.spec.Dir != "." || r.spec.Script != "start" {
		t.Errorf("spec: %+v", r.spec)
	}
}

func TestBuildRowsMissingProject(t *testing.T) {
	cfg := sampleConfig()
	cfg.Projects["gone"] = config.Project{Scripts: []string{".:x"}}
	rows := buildRows(sampleProjects(), true, cfg, map[string]bool{}, false, "")
	last := rows[len(rows)-1]
	if last.kind != rowProject || last.project != "gone" || !last.missing {
		t.Fatalf("last row: %+v", last)
	}
	before := buildRows(nil, false, cfg, map[string]bool{}, false, "")
	if len(before) != 0 {
		t.Fatalf("no rows before discovery, got: %v", labels(before))
	}
}
