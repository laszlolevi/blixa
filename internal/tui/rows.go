package tui

import (
	"sort"
	"strings"

	"blixa/internal/config"
	"blixa/internal/discovery"
	"blixa/internal/task"
)

type rowKind int

const (
	rowProject rowKind = iota
	rowTask
)

type row struct {
	kind    rowKind
	project string
	missing bool      // present in the config, but the folder is gone
	spec    task.Spec // rowTask only
}

// buildRows: the main view's rows. By default only projects with a selected script are
// shown; when filtering, rows matching the project name or the task's label.
func buildRows(projects []discovery.Project, scanned bool, cfg config.Config, collapsed map[string]bool, showAll bool, filter string) []row {
	filter = strings.ToLower(strings.TrimSpace(filter))
	var rows []row
	add := func(name, path string, missing bool) {
		var tasks []row
		if !missing {
			for _, ref := range cfg.Selected(name) {
				tasks = append(tasks, row{kind: rowTask, project: name, spec: task.Spec{
					Project: name, ProjectPath: path, Kind: task.KindScript, Dir: ref.Dir, Script: ref.Script,
				}})
			}
		}
		if !showAll && !missing && len(tasks) == 0 {
			return
		}
		head := row{kind: rowProject, project: name, missing: missing}
		if filter != "" && !strings.Contains(strings.ToLower(name), filter) {
			var matched []row
			for _, t := range tasks {
				if strings.Contains(strings.ToLower(t.spec.Label()), filter) {
					matched = append(matched, t)
				}
			}
			if len(matched) > 0 {
				rows = append(rows, head)
				rows = append(rows, matched...)
			}
			return
		}
		rows = append(rows, head)
		if !collapsed[name] {
			rows = append(rows, tasks...)
		}
	}
	if !scanned {
		return nil
	}
	known := map[string]bool{}
	for _, p := range projects {
		known[p.Name] = true
		add(p.Name, p.Path, false)
	}
	var missing []string
	for name := range cfg.Projects {
		if !known[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		add(name, "", true)
	}
	return rows
}
