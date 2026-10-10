package supervisor

import (
	"net"
	"os"
	"testing"
)

func TestParseListenPorts(t *testing.T) {
	out := "p4242\nf23\nn*:3001\nf24\nn127.0.0.1:9229\nf25\nn[::1]:3001\n"
	got := ParseListenPorts(out)
	if len(got) != 2 || got[0] != 3001 || got[1] != 9229 {
		t.Fatalf("ParseListenPorts = %v", got)
	}
	if len(ParseListenPorts("")) != 0 {
		t.Fatal("empty output should give an empty list")
	}
}

func TestParseOwner(t *testing.T) {
	o, ok := ParseOwner("p4242\ncnode\nf23\nn*:3001\n")
	if !ok || o.PID != 4242 || o.Command != "node" {
		t.Fatalf("ParseOwner = %+v, %v", o, ok)
	}
	if _, ok := ParseOwner(""); ok {
		t.Fatal("empty output should have no owner")
	}
}

func TestLookupPortOwnerAndGroupPorts(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	o, ok, err := lookupPortOwner(port)
	if err != nil || !ok || o.PID != os.Getpid() {
		t.Fatalf("lookupPortOwner = %+v, %v, %v", o, ok, err)
	}
	pgid, _ := syscallGetpgid(os.Getpid())
	ports, err := GroupListenPorts(pgid)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range ports {
		found = found || p == port
	}
	if !found {
		t.Fatalf("expected port %d in the group, got: %v", port, ports)
	}
}
