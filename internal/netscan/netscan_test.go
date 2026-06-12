package netscan

import (
	"reflect"
	"testing"
)

// canned lsof -FpcLn output: bun on 3001 (v4+v6), a command with spaces and
// parens, an entry with no port (ignored), and a malformed port (ignored).
const lsofFixture = `p7891
cbun
Lramin
f5
n*:3001
f6
n[::1]:3001
p576
cCursor Helper (Plugin)
Lramin
f31
n127.0.0.1:17143
p999
cweird
Lroot
f3
n/var/run/socket
f4
n*:notaport
`

func TestParseLsofOutput(t *testing.T) {
	raws := parseLsofOutput(lsofFixture)
	if len(raws) != 3 {
		t.Fatalf("want 3 raw listeners, got %d: %+v", len(raws), raws)
	}

	if raws[0].pid != 7891 || raws[0].port != 3001 || raws[0].addr != "*" || raws[0].cmd != "bun" || raws[0].user != "ramin" {
		t.Errorf("first listener wrong: %+v", raws[0])
	}
	if raws[1].port != 3001 || raws[1].addr != "[::1]" {
		t.Errorf("v6 listener wrong: %+v", raws[1])
	}
	if raws[2].cmd != "Cursor Helper (Plugin)" {
		t.Errorf("space-containing command parsed wrong: %q", raws[2].cmd)
	}
}

func TestDedupeMergesV4V6(t *testing.T) {
	ls := dedupe(parseLsofOutput(lsofFixture))
	if len(ls) != 2 {
		t.Fatalf("want 2 deduped listeners, got %d: %+v", len(ls), ls)
	}
	// sorted by port: 3001 first
	if ls[0].Port != 3001 || !reflect.DeepEqual(ls[0].Addrs, []string{"*", "[::1]"}) {
		t.Errorf("v4+v6 not merged: %+v", ls[0])
	}
	if ls[1].Port != 17143 {
		t.Errorf("want 17143 second, got %+v", ls[1])
	}
}

func TestSplitHostPort(t *testing.T) {
	cases := []struct {
		in   string
		port uint32
		addr string
		ok   bool
	}{
		{"127.0.0.1:3000", 3000, "127.0.0.1", true},
		{"*:5432", 5432, "*", true},
		{"[::1]:8080", 8080, "[::1]", true},
		{"[fe80::1%lo0]:631", 631, "[fe80::1%lo0]", true},
		{"noport", 0, "", false},
		{"*:0", 0, "", false},
		{"*:99999", 0, "", false},
		{"*:abc", 0, "", false},
	}
	for _, c := range cases {
		port, addr, ok := splitHostPort(c.in)
		if port != c.port || addr != c.addr || ok != c.ok {
			t.Errorf("splitHostPort(%q) = (%d, %q, %v), want (%d, %q, %v)",
				c.in, port, addr, ok, c.port, c.addr, c.ok)
		}
	}
}

func TestFilters(t *testing.T) {
	ls := []Listener{
		{Port: 3000, PID: 1, Name: "node", Cmdline: "node server.js"},
		{Port: 5432, PID: 2, Name: "postgres", Cmdline: "postgres -D /data"},
		{Port: 8080, PID: 3, Name: "java", Cmdline: "java -jar app.jar"},
	}

	got := FilterPorts(ls, []uint32{3000, 8080})
	if len(got) != 2 || got[0].Port != 3000 || got[1].Port != 8080 {
		t.Errorf("FilterPorts wrong: %+v", got)
	}

	got = FilterName(ls, "POSTGRES")
	if len(got) != 1 || got[0].Port != 5432 {
		t.Errorf("FilterName case-insensitivity wrong: %+v", got)
	}

	got = FilterName(ls, "server.js")
	if len(got) != 1 || got[0].Port != 3000 {
		t.Errorf("FilterName cmdline match wrong: %+v", got)
	}
}

func TestPortsForPID(t *testing.T) {
	ls := []Listener{
		{Port: 8080, PID: 7},
		{Port: 8081, PID: 7},
		{Port: 9000, PID: 8},
	}
	got := PortsForPID(ls, 7)
	if !reflect.DeepEqual(got, []uint32{8080, 8081}) {
		t.Errorf("PortsForPID = %v", got)
	}
}
