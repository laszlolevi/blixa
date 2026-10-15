// Package daemon is the `blixa daemon` main loop and the TUI-side start/restart helpers.
package daemon

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/laszlolevi/blixa/internal/api"
	"github.com/laszlolevi/blixa/internal/paths"
	"github.com/laszlolevi/blixa/internal/supervisor"
)

// Run: the daemon main loop; runs until a shutdown request or SIGTERM/SIGINT.
// Tasks keep running on shutdown (unless shutdown is called with stopAll=true).
func Run(ver string) error {
	stateDir := paths.StateDir()
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	sock := paths.SocketPath()
	if _, err := (&api.Client{Socket: sock}).Version(); err == nil {
		return errors.New("the daemon is already running")
	}

	// The Version() check has a TOCTOU gap (two daemons can start almost simultaneously,
	// before either creates the socket) -- the flock is exclusive for the process's whole
	// lifetime, guaranteeing that only one daemon can run on a given state dir.
	lockFile, err := os.OpenFile(filepath.Join(stateDir, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lockFile.Close()
		return errors.New("the daemon is already running")
	}
	defer func() {
		syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		lockFile.Close()
	}()

	_ = os.Remove(sock) // leftover from a previously uncleanly stopped daemon (only after acquiring the lock!)

	sup, err := supervisor.New(supervisor.Options{StateDir: stateDir})
	if err != nil {
		return err
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	if err := os.Chmod(sock, 0o600); err != nil {
		ln.Close()
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	go sup.Run(ctx)

	done := make(chan struct{})
	var once sync.Once
	shutdown := func(stopAll bool) {
		once.Do(func() {
			cancel()
			ln.Close() // the socket first, only then stop the tasks (if stopAll)
			if stopAll {
				sup.StopAll()
			}
			close(done)
		})
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigs
		shutdown(false)
	}()

	srv := &api.Server{Sup: sup, Version: ver, Shutdown: shutdown}
	go srv.Serve(ln)
	<-done
	signal.Stop(sigs)
	// no need for a separate os.Remove(sock): ln.Close() (net.UnixListener) already
	// removes the socket file.
	return nil
}

// EnsureRunning: connects to the daemon; if it isn't running, starts it in the background.
func EnsureRunning() (*api.Client, error) {
	c := &api.Client{Socket: paths.SocketPath()}
	if _, err := c.Version(); err == nil {
		return c, nil
	}
	if err := Spawn(); err != nil {
		return nil, err
	}
	if waitForVersion(c, 3*time.Second) {
		return c, nil
	}
	// The previous daemon may hold the flock for ~7 more seconds after a shutdown request,
	// while it stops its tasks -- meanwhile a newly spawned daemon exits with "the daemon
	// is already running". In that case we try a second Spawn, for a total budget of about 12 seconds.
	if err := Spawn(); err != nil {
		return nil, err
	}
	if waitForVersion(c, 9*time.Second) {
		return c, nil
	}
	return nil, errors.New("the daemon did not start -- see: " + filepath.Join(paths.StateDir(), "daemon.log"))
}

// waitForVersion: waits until the daemon responds to Version(), for at most `timeout`.
func waitForVersion(c *api.Client, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := c.Version(); err == nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// Spawn: starts `blixa daemon` in a new session, with output going to daemon.log.
func Spawn() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(paths.StateDir(), 0o700); err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(paths.StateDir(), "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(exe, "daemon")
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// Restart: stops the old daemon (leaving the tasks running), and starts a new one,
// which adopts the running tasks based on state.json.
func Restart(c *api.Client) (*api.Client, error) {
	_ = c.Shutdown(false)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := c.Version(); err != nil {
			break
		}
		if time.Now().After(deadline) {
			return nil, errors.New("the old daemon did not stop within the given time")
		}
		time.Sleep(50 * time.Millisecond)
	}
	return EnsureRunning()
}
