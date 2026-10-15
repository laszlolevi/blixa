package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseStatus(t *testing.T) {
	out := "# branch.oid abc\n# branch.head feature/x\n# branch.upstream origin/feature/x\n# branch.ab +1 -2\n1 .M N... 100644 100644 100644 a b file.go\n"
	st := parseStatus(out)
	if st.Branch != "feature/x" || !st.Upstream || st.Ahead != 1 || st.Behind != 2 || !st.Dirty {
		t.Fatalf("parseStatus = %+v", st)
	}
	clean := parseStatus("# branch.oid abc\n# branch.head main\n")
	if clean.Dirty || clean.Upstream || clean.Branch != "main" {
		t.Fatalf("clean = %+v", clean)
	}
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=blixa", "-c", "user.email=blixa@example.com", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func commitFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", name)
	gitCmd(t, dir, "commit", "-m", name)
}

func TestStatusBranchesFetch(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	gitCmd(t, base, "init", "--bare", "-b", "main", remote)

	work := filepath.Join(base, "work")
	gitCmd(t, base, "clone", remote, work)
	gitCmd(t, work, "symbolic-ref", "HEAD", "refs/heads/main") // branch name is not guaranteed in an empty clone
	commitFile(t, work, "a.txt", "a")
	gitCmd(t, work, "push", "-u", "origin", "main")

	other := filepath.Join(base, "other")
	gitCmd(t, base, "clone", remote, other)
	commitFile(t, other, "b.txt", "b")
	gitCmd(t, other, "push", "origin", "main")
	gitCmd(t, other, "checkout", "-b", "feature/x")
	gitCmd(t, other, "push", "origin", "feature/x")

	commitFile(t, work, "c.txt", "c") // local commit -> ahead 1
	if err := Fetch(work); err != nil {
		t.Fatal(err)
	}
	st, err := GetStatus(work)
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "main" || st.Ahead != 1 || st.Behind != 1 || st.Dirty {
		t.Fatalf("status = %+v", st)
	}

	if err := os.WriteFile(filepath.Join(work, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if st, _ := GetStatus(work); !st.Dirty {
		t.Fatal("expected dirty")
	}

	branches, err := Branches(work)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]Branch{}
	for _, b := range branches {
		names[b.FullName()] = b
	}
	if len(branches) != 3 || names["main"].Remote != "" || names["origin/main"].Remote != "origin" || names["origin/feature/x"].Date.IsZero() {
		t.Fatalf("branches = %+v", branches)
	}
	if names["main"].Hash == names["origin/main"].Hash {
		t.Fatal("main is ahead and behind: its hash differs from origin/main")
	}
	if n, err := LocalOnlyCommits(work, "main", "origin/main"); err != nil || n != 1 {
		t.Fatalf("LocalOnlyCommits = %d, %v (c.txt is only local)", n, err)
	}
}

func TestBranchesByName(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-b", "main")
	commitFile(t, dir, "a.txt", "a")
	gitCmd(t, dir, "branch", "Zeta")
	gitCmd(t, dir, "branch", "alpha")
	branches, err := Branches(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range branches {
		names = append(names, b.Name)
	}
	if got := strings.Join(names, ","); got != "alpha,main,Zeta" {
		t.Fatalf("by name, case-insensitively: %s", got)
	}
}
