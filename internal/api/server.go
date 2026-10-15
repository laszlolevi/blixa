package api

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"syscall"

	"github.com/laszlolevi/blixa/internal/supervisor"
	"github.com/laszlolevi/blixa/internal/task"
)

type Server struct {
	Sup      *supervisor.Supervisor
	Version  string
	Shutdown func(stopAll bool) // set by the daemon
}

func (srv *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go srv.handle(conn)
	}
}

func (srv *Server) handle(conn net.Conn) {
	defer conn.Close()
	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}
	enc := json.NewEncoder(conn)
	switch req.Op {
	case "version":
		enc.Encode(Response{Version: srv.Version})
	case "list":
		enc.Encode(Response{Tasks: srv.Sup.List()})
	case "start", "restart":
		if req.Spec == nil {
			enc.Encode(Response{Error: "missing spec"})
			return
		}
		var info task.Info
		var err error
		if req.Op == "start" {
			info, err = srv.Sup.Start(*req.Spec)
		} else {
			info, err = srv.Sup.Restart(*req.Spec)
		}
		resp := errResponse(err)
		resp.Task = &info
		enc.Encode(resp)
	case "stop":
		enc.Encode(errResponse(srv.Sup.Stop(req.ID)))
	case "portowner":
		owner, err := srv.Sup.PortOwner(req.Port)
		resp := errResponse(err)
		resp.Owner = owner
		enc.Encode(resp)
	case "kill":
		if req.PID <= 1 || req.PID == os.Getpid() {
			enc.Encode(Response{Error: "invalid pid: cannot kill the init process or the daemon"})
			return
		}
		enc.Encode(errResponse(syscall.Kill(req.PID, syscall.SIGTERM)))
	case "subscribe":
		srv.subscribe(conn, enc)
	case "log":
		srv.streamLog(conn, enc, req.ID)
	case "shutdown":
		enc.Encode(Response{})
		if srv.Shutdown != nil {
			go srv.Shutdown(req.StopAll)
		}
	default:
		enc.Encode(Response{Error: "unknown operation: " + req.Op})
	}
}

// clientGone closes when the client disconnects.
func clientGone(conn net.Conn) <-chan struct{} {
	gone := make(chan struct{})
	go func() {
		io.Copy(io.Discard, conn)
		close(gone)
	}()
	return gone
}

func (srv *Server) subscribe(conn net.Conn, enc *json.Encoder) {
	ch, cancel := srv.Sup.Subscribe()
	defer cancel()
	gone := clientGone(conn)
	for {
		select {
		case <-gone:
			return
		case tasks, ok := <-ch:
			if !ok {
				return
			}
			if enc.Encode(Response{Tasks: tasks}) != nil {
				return
			}
		}
	}
}

func (srv *Server) streamLog(conn net.Conn, enc *json.Encoder, id string) {
	path, ok := srv.Sup.LogPathFor(id)
	if !ok {
		enc.Encode(Response{Error: supervisor.ErrNotFound.Error()})
		return
	}
	_ = tailFile(path, 2000, func(line string) error {
		return enc.Encode(Response{Line: &line})
	}, clientGone(conn))
}
