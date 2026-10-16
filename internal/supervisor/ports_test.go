package supervisor

import (
	"fmt"
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

func TestMergePortsKeepsFirstSeenOrder(t *testing.T) {
	cases := []struct{ prev, found, want []int }{
		{nil, []int{3000, 9229}, []int{3000, 9229}},
		{[]int{5173}, []int{3000, 5173}, []int{5173, 3000}}, // 3000 opened later: it goes after
		{[]int{5173, 3000}, []int{3000}, []int{3000}},       // a closed port drops out
		{[]int{5173}, nil, nil},
	}
	for _, c := range cases {
		got := mergePorts(c.prev, c.found, nil)
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("mergePorts(%v, %v) = %v, want %v", c.prev, c.found, got, c.want)
		}
	}
}

func TestMergePortsPutsInspectorPortsLast(t *testing.T) {
	kinds := map[int]string{3000: "http", 3001: "http", 8080: "http", 9229: KindDebug, 9230: KindDebug}
	cases := []struct{ prev, found, want []int }{
		{nil, []int{3000, 9229}, []int{3000, 9229}},
		{[]int{9229}, []int{9229}, []int{9229}},                               // only the debugger is open yet
		{[]int{9229}, []int{3000, 9229}, []int{3000, 9229}},                   // the app server opened later: it still becomes primary
		{[]int{8080, 9230}, []int{3001, 8080, 9230}, []int{8080, 3001, 9230}}, // first-seen order among the rest
	}
	for _, c := range cases {
		got := mergePorts(c.prev, c.found, kinds)
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("mergePorts(%v, %v) = %v, want %v", c.prev, c.found, got, c.want)
		}
	}
}
