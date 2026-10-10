// Package git provides short, read-only git queries (status, branches, fetch).
package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

type Status struct {
	Branch   string
	Upstream bool
	Ahead    int
	Behind   int
	Dirty    bool
}

func GetStatus(dir string) (Status, error) {
	out, err := run(dir, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return Status{}, err
	}
	return parseStatus(out), nil
}

func parseStatus(out string) Status {
	var st Status
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			st.Branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.upstream "):
			st.Upstream = true
		case strings.HasPrefix(line, "# branch.ab "):
			fmt.Sscanf(strings.TrimPrefix(line, "# branch.ab "), "+%d -%d", &st.Ahead, &st.Behind)
		case line != "" && !strings.HasPrefix(line, "#"):
			st.Dirty = true
		}
	}
	return st
}

// Branches: local branches first, then remote-only ones (without the remote prefix).
func Branches(dir string) ([]string, error) {
	out, err := run(dir, "for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var local, remote []string
	for _, ref := range strings.Split(strings.TrimSpace(out), "\n") {
		switch {
		case strings.HasPrefix(ref, "refs/heads/"):
			name := strings.TrimPrefix(ref, "refs/heads/")
			seen[name] = true
			local = append(local, name)
		case strings.HasPrefix(ref, "refs/remotes/"):
			rest := strings.TrimPrefix(ref, "refs/remotes/")
			i := strings.Index(rest, "/")
			if i < 0 || rest[i+1:] == "HEAD" {
				continue
			}
			remote = append(remote, rest[i+1:])
		}
	}
	sort.Strings(local)
	sort.Strings(remote)
	result := local
	for _, name := range remote {
		if !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	return result, nil
}

func Fetch(dir string) error {
	_, err := run(dir, "fetch", "--prune")
	return err
}

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}
