package api

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"blixa/internal/supervisor"
	"blixa/internal/task"
)

func TestMain(m *testing.M) {
	if os.Getenv("BLIXA_HELPER") == "echo" {
		fmt.Println("hello")
		time.Sleep(time.Hour)
		return
	}
	os.Exit(m.Run())
}

func startServer(t *testing.T) *Client {
	t.Helper()
	exe, _ := os.Executable()
	sup, err := supervisor.New(supervisor.Options{
		StateDir:     t.TempDir(),
		Shell:        "/bin/sh",
		Command:      func(task.Spec) (string, error) { return "BLIXA_HELPER=echo exec '" + exe + "'", nil },
		PollInterval: 100 * time.Millisecond,
		StopTimeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("/tmp", "blixa-api-")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go sup.Run(ctx)
	srv := &Server{Sup: sup, Version: "v-test"}
	go srv.Serve(ln)
	t.Cleanup(func() {
		sup.StopAll()
		cancel()
		ln.Close()
		os.RemoveAll(dir)
	})
	return &Client{Socket: sock}
}

func waitSnapshot(t *testing.T, ch <-chan []task.Info, cond func([]task.Info) bool) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case snap, ok := <-ch:
			if !ok {
				t.Fatal("the subscription closed")
			}
			if cond(snap) {
				return
			}
		case <-deadline:
			t.Fatal("timeout")
		}
	}
}

func TestVersionAndUnknownOp(t *testing.T) {
	c := startServer(t)
	v, err := c.Version()
	if err != nil || v != "v-test" {
		t.Fatalf("Version = %q, %v", v, err)
	}
	if _, err := c.call(Request{Op: "nope"}); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("expected an error for an unknown operation, got: %v", err)
	}
}

// TestCallHasDeadline: a server that never responds must not be able to block the
// non-streaming calls (call) forever -- the connection has an upper bound.
func TestCallHasDeadline(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "blixa-cd-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(io.Discard, conn) // read the request, but never respond
	}()

	c := &Client{Socket: sock}
	start := time.Now()
	if _, err := c.Version(); err == nil {
		t.Fatal("expected an error for a server that never responds")
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Fatalf("the call blocked too long: %v", elapsed)
	}
}

func TestKillRejectsUnsafePID(t *testing.T) {
	c := startServer(t)
	if err := c.Kill(1); err == nil {
		t.Fatal("expected an error for Kill(1)")
	}
	if err := c.Kill(0); err == nil {
		t.Fatal("expected an error for Kill(0)")
	}
	if err := c.Kill(-5); err == nil {
		t.Fatal("expected an error for Kill(-5)")
	}
	if err := c.Kill(os.Getpid()); err == nil {
		t.Fatal("expected an error for Kill(own pid)")
	}
}

func TestStartSubscribeLogStop(t *testing.T) {
	c := startServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spec := task.Spec{Project: "p", ProjectPath: t.TempDir(), Kind: task.KindScript, Dir: ".", Script: "x"}
	info, err := c.Start(spec)
	if err != nil || info.Status != task.StatusStarting {
		t.Fatalf("Start = %+v, %v", info, err)
	}
	if _, err := c.Start(spec); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second Start: %v", err)
	}
	waitSnapshot(t, sub, func(s []task.Info) bool { return len(s) == 1 && s[0].Status.Active() })

	lines, err := c.StreamLog(ctx, spec.ID())
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for got := false; !got; {
		select {
		case l := <-lines:
			got = l == "hello"
		case <-deadline:
			t.Fatal("hello did not arrive from the log")
		}
	}

	if err := c.Stop(spec.ID()); err != nil {
		t.Fatal(err)
	}
	waitSnapshot(t, sub, func(s []task.Info) bool { return len(s) == 1 && s[0].Status == task.StatusStopped })

	list, err := c.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %+v, %v", list, err)
	}
	if owner, err := c.PortOwner(1); err != nil || owner != nil {
		t.Fatalf("PortOwner(1) = %+v, %v", owner, err)
	}
}
