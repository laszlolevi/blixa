// blixa: a terminal UI for managing npm, pnpm, yarn & bun projects.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/laszlolevi/blixa/internal/api"
	"github.com/laszlolevi/blixa/internal/config"
	"github.com/laszlolevi/blixa/internal/daemon"
	"github.com/laszlolevi/blixa/internal/discovery"
	"github.com/laszlolevi/blixa/internal/git"
	"github.com/laszlolevi/blixa/internal/paths"
	"github.com/laszlolevi/blixa/internal/task"
	"github.com/laszlolevi/blixa/internal/tui"
	"github.com/laszlolevi/blixa/internal/update"
	"github.com/laszlolevi/blixa/internal/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		if err := daemon.Run(version.Get()); err != nil {
			fmt.Fprintln(os.Stderr, "blixa daemon:", err)
			os.Exit(1)
		}
		return
	}
	root := flag.String("root", "", "projects root directory (saved to the config)")
	flag.Parse()
	if err := runTUI(*root); err != nil {
		fmt.Fprintln(os.Stderr, "blixa:", err)
		os.Exit(1)
	}
}

func runTUI(rootFlag string) error {
	cfgPath := paths.ConfigFile()
	cfg, cfgErr := config.Load(cfgPath)
	if cfgErr == nil {
		if err := configureRoot(&cfg, cfgPath, rootFlag, os.Stdin, os.Stdout); err != nil {
			return err
		}
	} else if rootFlag != "" {
		return fmt.Errorf("cannot set root, the config is invalid: %w", cfgErr)
	}

	if err := maybeUpdate(defaultUpdateDeps(), os.Stdin, os.Stdout); err != nil {
		return err
	}

	ver := version.Get()
	c, err := daemon.EnsureRunning()
	if err != nil {
		return err
	}
	if dv, err := c.Version(); err == nil && dv != ver {
		fmt.Printf("The daemon is running an older blixa binary (%s, now: %s).\nRestart it? Running tasks will be kept. [Y/n] ", dv, ver)
		var ans string
		fmt.Scanln(&ans)
		if ans == "" || strings.EqualFold(ans, "y") {
			if c, err = daemon.Restart(c); err != nil {
				return err
			}
		}
	}

	deps := tui.Deps{
		Client:     c,
		ConfigPath: cfgPath,
		StateDir:   paths.StateDir(),
		Splash:     true,
		Version:    version.Tag(),
		Subscribe:  func() (<-chan []task.Info, error) { return c.Subscribe(context.Background()) },
		StreamLog:  c.StreamLog,
		Scan:       discovery.Scan,
		GitStatus:  git.GetStatus,
		Fetch:      git.Fetch,
		LocalOnly:  git.LocalOnlyCommits,
		Branches: func(dir string) ([]git.Branch, error) {
			_ = git.Fetch(dir) // on a network error, list from local knowledge
			return git.Branches(dir)
		},
	}
	_, err = tea.NewProgram(tui.New(deps, cfg, cfgErr), tea.WithAltScreen()).Run()
	return err
}

// configureRoot saves the root given with --root; on the very first run (no config file
// yet) it asks where the projects live instead, suggesting the working directory.
func configureRoot(cfg *config.Config, cfgPath, rootFlag string, in io.Reader, out io.Writer) error {
	root := rootFlag
	if root == "" {
		if _, err := os.Stat(cfgPath); err == nil {
			return nil
		}
		suggested := cfg.Root
		if wd, err := os.Getwd(); err == nil {
			if _, err := os.Stat(filepath.Join(wd, "package.json")); err == nil {
				wd = filepath.Dir(wd) // started inside a project: its parent holds the projects
			}
			suggested = paths.Collapse(wd)
		}
		fmt.Fprintf(out, "Where are your projects? [%s] ", suggested)
		line, _ := bufio.NewReader(in).ReadString('\n')
		if root = strings.TrimSpace(line); root == "" {
			root = suggested
		}
	}
	abs, err := filepath.Abs(paths.Expand(root))
	if err != nil {
		return err
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return fmt.Errorf("not a directory: %s", root)
	}
	cfg.SetRoot(paths.Collapse(abs))
	return config.Save(cfgPath, *cfg)
}

// updateDeps: what maybeUpdate needs from the outside world (replaced in tests).
type updateDeps struct {
	DaemonRunning func() bool
	Current       string // version.Tag() of the running binary
	Latest        func() (string, error)
	Apply         func(tag string) error
	Exec          func() error // re-executes the (replaced) binary; returns only on failure
}

func defaultUpdateDeps() updateDeps {
	u := update.Default()
	exe, exeErr := os.Executable()
	if exeErr == nil {
		exe, exeErr = filepath.EvalSymlinks(exe) // replace the real file, not a symlink to it
	}
	return updateDeps{
		DaemonRunning: func() bool {
			_, err := (&api.Client{Socket: paths.SocketPath()}).Version()
			return err == nil
		},
		Current: version.Tag(),
		Latest: func() (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			return u.Latest(ctx)
		},
		Apply: func(tag string) error {
			if exeErr != nil {
				return exeErr
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			return u.Apply(ctx, tag, exe)
		},
		Exec: func() error { return syscall.Exec(exe, os.Args, os.Environ()) },
	}
}

// maybeUpdate: when no daemon is running (so nothing runs the old binary), offers a newer
// release and on yes replaces the binary and re-executes it. A failed check stays silent,
// a failed update continues with the current version.
func maybeUpdate(d updateDeps, in io.Reader, out io.Writer) error {
	if d.DaemonRunning() {
		return nil
	}
	latest, err := d.Latest()
	if err != nil || !update.Newer(d.Current, latest) {
		return nil
	}
	fmt.Fprintf(out, "blixa %s is available (you have %s). Update now? [Y/n] ", latest, d.Current)
	line, _ := bufio.NewReader(in).ReadString('\n')
	if ans := strings.TrimSpace(line); ans != "" && !strings.EqualFold(ans, "y") {
		return nil
	}
	fmt.Fprintf(out, "downloading %s…\n", latest)
	if err := d.Apply(latest); err != nil {
		fmt.Fprintf(out, "blixa: update failed: %v\n", err)
		return nil
	}
	fmt.Fprintf(out, "updated to %s\n", latest)
	if err := d.Exec(); err != nil {
		return fmt.Errorf("updated to %s, but could not restart (%v) -- run blixa again", latest, err)
	}
	return nil
}
