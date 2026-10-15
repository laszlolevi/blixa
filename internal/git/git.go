// Package git provides short, read-only git queries (status, branches, fetch).
package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
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

// Branch: a local branch, or a remote-tracking one (Remote set, Name without the
// remote's prefix). Date is its latest commit.
type Branch struct {
	Name   string
	Remote string // e.g. "origin"; "" for a local branch
	Hash   string
	Date   time.Time
}

// FullName: "origin/feature/x" for a remote branch, the name for a local one.
func (b Branch) FullName() string {
	if b.Remote == "" {
		return b.Name
	}
	return b.Remote + "/" + b.Name
}

// Branches: every local and remote-tracking branch, by name (case-insensitively).
func Branches(dir string) ([]Branch, error) {
	out, err := run(dir, "for-each-ref", "--format=%(refname)%09%(objectname)%09%(committerdate:unix)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	var result []Branch
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			continue
		}
		var b Branch
		switch ref := f[0]; {
		case strings.HasPrefix(ref, "refs/heads/"):
			b.Name = strings.TrimPrefix(ref, "refs/heads/")
		case strings.HasPrefix(ref, "refs/remotes/"):
			remote, name, ok := strings.Cut(strings.TrimPrefix(ref, "refs/remotes/"), "/")
			if !ok || name == "HEAD" {
				continue
			}
			b.Remote, b.Name = remote, name
		default:
			continue
		}
		sec, _ := strconv.ParseInt(f[2], 10, 64)
		b.Hash, b.Date = f[1], time.Unix(sec, 0)
		result = append(result, b)
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := strings.ToLower(result[i].FullName()), strings.ToLower(result[j].FullName())
		if a != b {
			return a < b
		}
		return result[i].FullName() < result[j].FullName()
	})
	return result, nil
}

// LocalOnlyCommits: how many commits of local have no equivalent in upstream -- the ones
// that resetting local to upstream would drop. A rebased-away copy of a commit that is in
// upstream (same patch) doesn't count.
func LocalOnlyCommits(dir, local, upstream string) (int, error) {
	out, err := run(dir, "cherry", upstream, local)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "+") {
			n++
		}
	}
	return n, nil
}

// fetchTimeout: a fetch also runs in the background periodically, so a hanging remote
// must not keep it going forever.
const fetchTimeout = 2 * time.Minute

// Fetch never asks for anything: no credential prompt, and ssh in batch mode (no
// passphrase / host key prompt) unless GIT_SSH_COMMAND is set already.
func Fetch(dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	var env []string
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	_, err := runContext(ctx, dir, env, "fetch", "--prune")
	return err
}

func run(dir string, args ...string) (string, error) {
	return runContext(context.Background(), dir, nil, args...)
}

func runContext(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), env...)
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
