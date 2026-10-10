package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"blixa/internal/task"
)

func TestShellQuote(t *testing.T) {
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Fatalf("shellQuote = %s", got)
	}
}

func TestBuildCommandScriptWithoutNvmrc(t *testing.T) {
	dir := t.TempDir()
	cmd, err := BuildCommand(task.Spec{ProjectPath: dir, Kind: task.KindScript, Dir: ".", Script: "build-all:prod"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cmd, "npm run 'build-all:prod'") {
		t.Errorf("command: %s", cmd)
	}
	if !strings.Contains(cmd, `. "$NVM_DIR/nvm.sh"`) || strings.Contains(cmd, "nvm use") {
		t.Errorf("nvm load yes, nvm use not needed: %s", cmd)
	}
}

func TestBuildCommandScriptWithNvmrc(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".nvmrc"), []byte("v12\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd, err := BuildCommand(task.Spec{ProjectPath: dir, Kind: task.KindInstall})
	if err != nil {
		t.Fatal(err)
	}
	nvmrc := shellQuote(filepath.Join(dir, ".nvmrc"))
	if !strings.Contains(cmd, `nvm use "$(cat `+nvmrc+`)"`) || !strings.Contains(cmd, "nvm install") || !strings.HasSuffix(cmd, "npm install") {
		t.Errorf("command: %s", cmd)
	}
}

func TestBuildCommandGitAndErrors(t *testing.T) {
	if cmd, _ := BuildCommand(task.Spec{Kind: task.KindPull}); cmd != "git pull" {
		t.Errorf("pull: %s", cmd)
	}
	if cmd, _ := BuildCommand(task.Spec{Kind: task.KindCheckout, Branch: "feature/x"}); cmd != "git checkout 'feature/x'" {
		t.Errorf("checkout: %s", cmd)
	}
	if _, err := BuildCommand(task.Spec{Kind: task.KindCheckout}); err == nil {
		t.Error("empty branch: expected an error")
	}
	if _, err := BuildCommand(task.Spec{Kind: task.KindScript}); err == nil {
		t.Error("empty script: expected an error")
	}
	if _, err := BuildCommand(task.Spec{Kind: "nope"}); err == nil {
		t.Error("unknown kind: expected an error")
	}
}

// runNvmrcCommand runs the command built for an `npm install` task in a project with an
// .nvmrc, using a fake npm on PATH and (optionally) a fake nvm with the given exit code.
func runNvmrcCommand(t *testing.T, fakeNvmExit int, withNvm bool) (string, int) {
	t.Helper()
	project := t.TempDir()
	os.WriteFile(filepath.Join(project, ".nvmrc"), []byte("v12\n"), 0o644)
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "npm"), []byte("#!/bin/sh\necho fake npm \"$@\"\n"), 0o755)
	home := t.TempDir()
	if withNvm {
		nvmDir := filepath.Join(home, ".nvm")
		os.MkdirAll(nvmDir, 0o755)
		script := fmt.Sprintf("nvm() { echo \"fake nvm $*\"; return %d; }\n", fakeNvmExit)
		os.WriteFile(filepath.Join(nvmDir, "nvm.sh"), []byte(script), 0o644)
	}
	cmdline, err := BuildCommand(task.Spec{ProjectPath: project, Kind: task.KindInstall})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", cmdline)
	cmd.Dir = project
	cmd.Env = []string{"HOME=" + home, "PATH=" + bin + ":/usr/bin:/bin"}
	out, _ := cmd.CombinedOutput()
	return string(out), cmd.ProcessState.ExitCode()
}

func TestNvmrcWithoutNvmUsesSystemNode(t *testing.T) {
	out, code := runNvmrcCommand(t, 0, false)
	if code != 0 || !strings.Contains(out, "nvm is not installed") || !strings.Contains(out, "fake npm install") {
		t.Fatalf("exit=%d output:\n%s", code, out)
	}
}

func TestNvmrcWithNvmSwitchesVersion(t *testing.T) {
	out, code := runNvmrcCommand(t, 0, true)
	if code != 0 || !strings.Contains(out, "fake nvm use v12") || !strings.Contains(out, "fake npm install") {
		t.Fatalf("exit=%d output:\n%s", code, out)
	}
}

func TestNvmrcWithMissingVersionFails(t *testing.T) {
	out, code := runNvmrcCommand(t, 3, true)
	if code != 1 || !strings.Contains(out, "nvm install v12 required") || strings.Contains(out, "fake npm") {
		t.Fatalf("exit=%d output:\n%s", code, out)
	}
}
