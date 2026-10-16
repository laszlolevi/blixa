package supervisor

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// KindDebug: a debugger port (the Node/V8 inspector, e.g. `node --inspect`).
const KindDebug = "debug"

// probeTimeout: how long a single probe step waits for the port's answer.
const probeTimeout = 300 * time.Millisecond

// ProbePort: what speaks on the local TCP port -- ssh, mysql, smtp, ftp, pop3, imap
// (servers that greet first), mqtt, https, wss, http, ws, debug, grpc (a server that
// only speaks HTTP/2), or tcp if none of these. Each step opens its own connection:
// first we only listen (so a server-first protocol never gets bytes it doesn't
// understand), then MQTT goes before HTTP because e.g. Redis takes an HTTP request for
// an attack, while an HTTP server simply answers an MQTT packet with 400; and TLS goes
// before plain HTTP because an HTTPS server answers a plain request with 400 too.
func ProbePort(port int) string {
	addr := net.JoinHostPort("localhost", strconv.Itoa(port))
	if kind, ok := probeBanner(addr); ok {
		return kind
	}
	if probeMQTT(addr) {
		return "mqtt"
	}
	if kind, ok := probeHTTP(addr, &tls.Config{InsecureSkipVerify: true}); ok {
		switch kind {
		case "http":
			return "https"
		case "ws":
			return "wss"
		}
		return kind
	}
	if kind, ok := probeHTTP(addr, nil); ok {
		return kind
	}
	if probeH2(addr) {
		return "grpc"
	}
	return "tcp"
}

func probeDial(addr string) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", addr, probeTimeout)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(time.Now().Add(probeTimeout))
	return c, nil
}

// probeBanner: the protocol of a server that speaks first, recognized from its greeting.
func probeBanner(addr string) (string, bool) {
	c, err := probeDial(addr)
	if err != nil {
		return "", false
	}
	defer c.Close()
	b := make([]byte, 256)
	n, _ := c.Read(b)
	if n == 0 {
		return "", false
	}
	b = b[:n]
	switch {
	case bytes.HasPrefix(b, []byte("SSH-")):
		return "ssh", true
	case len(b) > 4 && b[3] == 0 && b[4] == 0x0a: // packet #0, protocol version 10
		return "mysql", true
	case bytes.HasPrefix(b, []byte("220")):
		if bytes.Contains(bytes.ToUpper(b), []byte("FTP")) {
			return "ftp", true
		}
		return "smtp", true
	case bytes.HasPrefix(b, []byte("+OK")):
		return "pop3", true
	case bytes.HasPrefix(b, []byte("* OK")):
		return "imap", true
	}
	return "tcp", true
}

// mqttConnect: an MQTT 3.1.1 CONNECT packet (clean session, client id "blixa"), which
// MQTT 5 brokers accept too.
var mqttConnect = []byte{
	0x10, 17, // CONNECT, remaining length
	0, 4, 'M', 'Q', 'T', 'T', 4, 0x02, 0, 60, // protocol name and level, flags, keep alive
	0, 5, 'b', 'l', 'i', 'x', 'a', // client id
}

func probeMQTT(addr string) bool {
	c, err := probeDial(addr)
	if err != nil {
		return false
	}
	defer c.Close()
	if _, err := c.Write(mqttConnect); err != nil {
		return false
	}
	b := make([]byte, 4)
	if _, err := io.ReadFull(c, b); err != nil || b[0] != 0x20 || b[1] != 2 { // CONNACK
		return false
	}
	c.Write([]byte{0xe0, 0}) // DISCONNECT
	return true
}

// probeHTTP: http, ws (a server that only takes WebSocket upgrades) or debug -- over
// TLS when cfg isn't nil.
func probeHTTP(addr string, cfg *tls.Config) (string, bool) {
	c, err := probeDial(addr)
	if err != nil {
		return "", false
	}
	defer c.Close()
	if cfg != nil {
		tc := tls.Client(c, cfg)
		if tc.Handshake() != nil {
			return "", false
		}
		c = tc
	}
	if _, err := io.WriteString(c, "GET /json/version HTTP/1.1\r\nHost: "+addr+"\r\nConnection: close\r\n\r\n"); err != nil {
		return "", false
	}
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUpgradeRequired || strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") {
		return "ws", true
	}
	var v struct {
		Browser         string
		ProtocolVersion string `json:"Protocol-Version"`
	}
	if resp.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&v) == nil &&
		v.Browser != "" && v.ProtocolVersion != "" {
		return KindDebug, true
	}
	return "http", true
}

// probeH2: whether the server speaks HTTP/2 without TLS -- in a dev setup that's a
// gRPC server.
func probeH2(addr string) bool {
	c, err := probeDial(addr)
	if err != nil {
		return false
	}
	defer c.Close()
	settings := []byte{0, 0, 0, 0x04, 0, 0, 0, 0, 0} // an empty SETTINGS frame
	if _, err := c.Write(append([]byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"), settings...)); err != nil {
		return false
	}
	b := make([]byte, 9)
	_, err = io.ReadFull(c, b)
	return err == nil && b[3] == 0x04
}
