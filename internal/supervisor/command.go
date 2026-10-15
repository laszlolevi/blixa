package supervisor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blixa/internal/discovery"
	"blixa/internal/task"
)

// nvmLoad loads nvm (if installed), so npm runs with the default Node version
// even if the login shell doesn't load it (e.g. because nvm is only set up in .zshrc).
const nvmLoad = `export NVM_DIR="${NVM_DIR:-$HOME/.nvm}"; [ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"; `

// BuildCommand: the shell command for the task (the login shell's `-lc` argument).
func BuildCommand(s task.Spec) (string, error) {
	pm := discovery.DetectPackageManager(s.ProjectPath)
	var cmd string
	switch s.Kind {
	case task.KindPull:
		return "git pull", nil
	case task.KindCheckout:
		if s.Branch == "" {
			return "", errors.New("missing branch")
		}
		if s.From != "" {
			// -B: creates the branch, or resets an existing one, at From (tracking it, as
			// From is a remote branch)
			return "git checkout -B " + shellQuote(s.Branch) + " " + shellQuote(s.From), nil
		}
		return "git checkout " + shellQuote(s.Branch), nil
	case task.KindInstall:
		cmd = pm + " install"
	case task.KindScript:
		if s.Script == "" {
			return "", errors.New("missing script name")
		}
		cmd = pm + " run " + shellQuote(s.Script)
	default:
		return "", fmt.Errorf("unknown task kind: %q", s.Kind)
	}
	// The package manager may come from nvm (or corepack under it), so check it after nvm is set up.
	cmd = `command -v ` + pm + ` >/dev/null 2>&1 || { echo "blixa: ` + pm + ` is not installed"; exit 127; }; ` + cmd
	nvmrc := filepath.Join(s.ProjectPath, ".nvmrc")
	if _, err := os.Stat(nvmrc); err == nil {
		// The .nvmrc is only honoured when nvm is available; without nvm the system
		// node/npm is used instead of failing the task.
		q := shellQuote(nvmrc)
		cmd = `if command -v nvm >/dev/null 2>&1; then ` +
			`nvm use "$(cat ` + q + `)" || { echo "blixa: nvm install $(cat ` + q + `) required"; exit 1; }; ` +
			`else echo "blixa: .nvmrc found but nvm is not installed, using the system node"; fi; ` + cmd
	}
	return nvmLoad + cmd, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
