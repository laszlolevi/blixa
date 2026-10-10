package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"

	"blixa/internal/supervisor"
	"blixa/internal/task"
)

type Client struct {
	Socket string
}

func (c *Client) open(req Request) (net.Conn, error) {
	conn, err := net.DialTimeout("unix", c.Socket, 2*time.Second)
	if err != nil {
		return nil, err
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func (c *Client) call(req Request) (Response, error) {
	conn, err := c.open(req)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	// A non-streaming call: the response is expected within this much time (Stop can
	// block server-side for up to ~7s), after which we return an error instead of blocking forever.
	if err := conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, err
	}
	if resp.Error != "" {
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}

func (c *Client) Version() (string, error) {
	resp, err := c.call(Request{Op: "version"})
	return resp.Version, err
}

func (c *Client) List() ([]task.Info, error) {
	resp, err := c.call(Request{Op: "list"})
	return resp.Tasks, err
}

func (c *Client) Start(spec task.Spec) (task.Info, error) { return c.startLike("start", spec) }

func (c *Client) Restart(spec task.Spec) (task.Info, error) { return c.startLike("restart", spec) }

func (c *Client) startLike(op string, spec task.Spec) (task.Info, error) {
	resp, err := c.call(Request{Op: op, Spec: &spec})
	if err != nil {
		return task.Info{}, err
	}
	if resp.Task == nil {
		return task.Info{}, errors.New("empty response")
	}
	return *resp.Task, nil
}

func (c *Client) Stop(id string) error {
	_, err := c.call(Request{Op: "stop", ID: id})
	return err
}

func (c *Client) PortOwner(port int) (*supervisor.PortOwner, error) {
	resp, err := c.call(Request{Op: "portowner", Port: port})
	return resp.Owner, err
}

func (c *Client) Kill(pid int) error {
	_, err := c.call(Request{Op: "kill", PID: pid})
	return err
}

func (c *Client) Shutdown(stopAll bool) error {
	_, err := c.call(Request{Op: "shutdown", StopAll: stopAll})
	return err
}

// Subscribe: a channel of state snapshots; it closes when the connection drops.
func (c *Client) Subscribe(ctx context.Context) (<-chan []task.Info, error) {
	conn, err := c.open(Request{Op: "subscribe"})
	if err != nil {
		return nil, err
	}
	ch := make(chan []task.Info)
	go func() {
		stop := context.AfterFunc(ctx, func() { conn.Close() })
		defer stop()
		defer conn.Close()
		defer close(ch)
		dec := json.NewDecoder(conn)
		for {
			var resp Response
			if dec.Decode(&resp) != nil {
				return
			}
			select {
			case ch <- resp.Tasks:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

// StreamLog: the task's log lines (the backlog first, then live).
func (c *Client) StreamLog(ctx context.Context, id string) (<-chan string, error) {
	conn, err := c.open(Request{Op: "log", ID: id})
	if err != nil {
		return nil, err
	}
	ch := make(chan string, 256)
	go func() {
		stop := context.AfterFunc(ctx, func() { conn.Close() })
		defer stop()
		defer conn.Close()
		defer close(ch)
		dec := json.NewDecoder(conn)
		for {
			var resp Response
			if dec.Decode(&resp) != nil {
				return
			}
			var line string
			switch {
			case resp.Error != "":
				line = "blixa: " + resp.Error
			case resp.Line != nil:
				line = *resp.Line
			default:
				continue
			}
			select {
			case ch <- line:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}
