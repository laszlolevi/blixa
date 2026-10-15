package daemon

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/laszlolevi/blixa/internal/api"
	"github.com/laszlolevi/blixa/internal/paths"
)

func TestRunServesAndShutsDown(t *testing.T) {
	parent, err := os.MkdirTemp("/tmp", "blixa-d-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(parent) })
	// the state dir itself doesn't exist yet -- so we can verify that Run creates it with 0700
	// (on an already-existing directory, MkdirAll wouldn't change the permissions).
	dir := filepath.Join(parent, "state")
	t.Setenv("BLIXA_STATE_DIR", dir)

	errc := make(chan error, 1)
	go func() { errc <- Run("v-test") }()

	c := &api.Client{Socket: paths.SocketPath()}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if v, err := c.Version(); err == nil && v == "v-test" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the daemon did not respond")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := Run("v-other"); err == nil {
		t.Fatal("a second daemon must not be able to start")
	}

	if fi, err := os.Stat(paths.SocketPath()); err != nil {
		t.Fatal(err)
	} else if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("the socket permission = %o, want: 600", perm)
	}
	if fi, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	} else if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Fatalf("the state dir permission = %o, want: 700", perm)
	}

	if err := c.Shutdown(false); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}
	if _, err := os.Stat(paths.SocketPath()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("the socket remained")
	}
}

// TestRunRequiresLock: Run must also return an error if daemon.lock is already held
// (via flock) by someone else -- this guards against the TOCTOU gap in the Version()-based
// check (two daemons starting almost simultaneously, before either creates the socket).
func TestRunRequiresLock(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "blixa-dl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("BLIXA_STATE_DIR", dir)

	lf, err := os.OpenFile(filepath.Join(dir, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}

	errc := make(chan error, 1)
	go func() { errc <- Run("v-locked") }()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("Run should have returned an error with daemon.lock already held")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return -- it ignored the lock")
	}
}

// TestRestartFailsIfOldDaemonIgnoresShutdown: if the old daemon still responds to
// Version() after the Shutdown() call (e.g. no Shutdown callback wired up), Restart must
// return an error instead of silently returning the old client.
//
// IMPORTANT: we deliberately put the socket at BLIXA_STATE_DIR's paths.SocketPath(), so that
// EnsureRunning()'s internal Version() check also finds the same (still alive) server
// connection, and never reaches Spawn() (which must not be called from a test).
func TestRestartFailsIfOldDaemonIgnoresShutdown(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "blixa-dr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("BLIXA_STATE_DIR", dir)

	sock := paths.SocketPath()
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	srv := &api.Server{Version: "v-stubborn"} // no Shutdown callback -- never stops
	go srv.Serve(ln)

	c := &api.Client{Socket: sock}
	if v, err := c.Version(); err != nil || v != "v-stubborn" {
		t.Fatalf("Version = %q, %v", v, err)
	}

	if _, err := Restart(c); err == nil {
		t.Fatal("Restart should have returned an error if the old daemon did not stop")
	}
}
