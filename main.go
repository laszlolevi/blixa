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

	tea "github.com/charmbracelet/bubbletea"

	"blixa/internal/config"
	"blixa/internal/daemon"
	"blixa/internal/discovery"
	"blixa/internal/git"
	"blixa/internal/paths"
	"blixa/internal/task"
	"blixa/internal/tui"
	"blixa/internal/version"
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
		Branches: func(dir string) ([]string, error) {
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
