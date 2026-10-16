package supervisor

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeServer: a TCP server that runs handle on every connection.
func fakeServer(t *testing.T, handle func(net.Conn)) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				handle(c)
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func httpPort(t *testing.T, h http.HandlerFunc) int {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().(*net.TCPAddr).Port
}

func TestProbePort(t *testing.T) {
	read := func(c net.Conn) []byte {
		buf := make([]byte, 1024)
		n, _ := c.Read(buf)
		return buf[:n]
	}
	tlsSrv := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(tlsSrv.Close)
	wssSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUpgradeRequired)
	}))
	t.Cleanup(wssSrv.Close)

	cases := []struct {
		name string
		port int
		want string
	}{
		{"http", httpPort(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }), "http"},
		{"debug", httpPort(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/json/version" {
				fmt.Fprint(w, `{"Browser": "node.js/v24.15.0", "Protocol-Version": "1.1"}`)
			}
		}), "debug"},
		{"ws", httpPort(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUpgradeRequired) // what the `ws` package answers a plain request with
		}), "ws"},
		{"https", tlsSrv.Listener.Addr().(*net.TCPAddr).Port, "https"},
		{"wss", wssSrv.Listener.Addr().(*net.TCPAddr).Port, "wss"},
		{"ssh", fakeServer(t, func(c net.Conn) { fmt.Fprint(c, "SSH-2.0-OpenSSH_9.8\r\n"); read(c) }), "ssh"},
		{"mysql", fakeServer(t, func(c net.Conn) {
			c.Write([]byte{0x4a, 0, 0, 0, 0x0a, '8', '.', '4', 0})
			read(c)
		}), "mysql"},
		{"mqtt", fakeServer(t, func(c net.Conn) {
			if b := read(c); len(b) > 0 && b[0] == 0x10 {
				c.Write([]byte{0x20, 0x02, 0x00, 0x00})
			}
		}), "mqtt"},
		{"grpc", fakeServer(t, func(c net.Conn) {
			if bytes.HasPrefix(read(c), []byte("PRI * HTTP/2.0")) {
				c.Write([]byte{0, 0, 0, 0x04, 0, 0, 0, 0, 0}) // an empty SETTINGS frame
				read(c)
			}
		}), "grpc"},
		{"tcp", fakeServer(t, func(c net.Conn) { read(c) }), "tcp"},
	}
	for _, c := range cases {
		if got := ProbePort(c.port); got != c.want {
			t.Errorf("%s: ProbePort = %q, want %q", c.name, got, c.want)
		}
	}
}
