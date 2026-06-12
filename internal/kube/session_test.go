package kube

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSpecValidate(t *testing.T) {
	cases := []struct {
		spec Spec
		ok   bool
	}{
		{Spec{Target: "svc/api", Ports: []string{"8080:80"}}, true},
		{Spec{Target: "pod/web-0", Ports: []string{"5432"}}, true},
		{Spec{Target: "deploy/web", Ports: []string{"8080:80", "9090:90"}}, true},
		{Spec{Target: "", Ports: []string{"8080"}}, false},
		{Spec{Target: "svc/api", Ports: nil}, false},
		{Spec{Target: "svc/api", Ports: []string{"80a0"}}, false},
		{Spec{Target: "svc/api", Ports: []string{":80"}}, false},
		{Spec{Target: "svc/api", Ports: []string{"8080:"}}, false},
	}
	for _, c := range cases {
		err := c.spec.Validate()
		if (err == nil) != c.ok {
			t.Errorf("Validate(%+v) err=%v, want ok=%v", c.spec, err, c.ok)
		}
	}
}

func TestSpecLabel(t *testing.T) {
	s := Spec{Context: "prod", Namespace: "staging", Target: "svc/api", Ports: []string{"8080:80"}}
	want := "svc/api 8080:80 -n staging @prod"
	if got := s.Label(); got != want {
		t.Errorf("Label() = %q, want %q", got, want)
	}
}

// fakeKubectl writes a script that prints the port-forward banner and then
// blocks (or exits, per mode), letting us test the session state machine
// without a cluster.
func fakeKubectl(t *testing.T, mode string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "kubectl")
	var body string
	switch mode {
	case "connect-and-hold":
		body = "#!/bin/sh\necho 'Forwarding from 127.0.0.1:8080 -> 80'\nsleep 60\n"
	case "connect-then-exit":
		body = "#!/bin/sh\necho 'Forwarding from 127.0.0.1:8080 -> 80'\nexit 1\n"
	case "fail":
		body = "#!/bin/sh\necho 'error: pod not found' >&2\nexit 1\n"
	}
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

func TestSessionConnects(t *testing.T) {
	mgr := NewManager()
	mgr.Kubectl = fakeKubectl(t, "connect-and-hold")

	s, err := mgr.Start(Spec{Target: "svc/api", Ports: []string{"8080:80"}})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.StopAll()

	if !waitFor(t, 3*time.Second, func() bool { return s.Status() == StatusConnected }) {
		t.Fatalf("session never connected; status=%s logs=%v", s.Status(), s.Logs())
	}

	// connected event was emitted
	select {
	case e := <-mgr.Events():
		if e.Kind != EventConnected {
			t.Errorf("first event = %s, want connected", e.Kind)
		}
	case <-time.After(time.Second):
		t.Error("no connected event")
	}
}

func TestSessionReconnects(t *testing.T) {
	mgr := NewManager()
	mgr.Kubectl = fakeKubectl(t, "connect-then-exit")

	s, err := mgr.Start(Spec{Target: "svc/api", Ports: []string{"8080:80"}})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.StopAll()

	// the fake exits immediately after connecting, so restarts must climb
	if !waitFor(t, 5*time.Second, func() bool { return s.Restarts() >= 1 }) {
		t.Fatalf("session never attempted reconnect; status=%s restarts=%d", s.Status(), s.Restarts())
	}
}

func TestSessionStop(t *testing.T) {
	mgr := NewManager()
	mgr.Kubectl = fakeKubectl(t, "connect-and-hold")

	s, err := mgr.Start(Spec{Target: "svc/api", Ports: []string{"8080:80"}})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool { return s.Status() == StatusConnected })

	if err := mgr.Stop(s.ID); err != nil {
		t.Fatal(err)
	}
	if s.Status() != StatusStopped {
		t.Errorf("status after stop = %s", s.Status())
	}
}

func TestStartValidates(t *testing.T) {
	mgr := NewManager()
	if _, err := mgr.Start(Spec{}); err == nil {
		t.Error("empty spec should fail validation")
	}
}

func TestStartMissingKubectl(t *testing.T) {
	mgr := NewManager()
	mgr.Kubectl = "definitely-not-a-real-binary-xyz"
	if _, err := mgr.Start(Spec{Target: "svc/api", Ports: []string{"80"}}); err == nil {
		t.Error("missing kubectl should fail fast")
	}
}

func TestRing(t *testing.T) {
	r := newRing(3)
	for _, s := range []string{"a", "b", "c", "d", "e"} {
		r.append(s)
	}
	got := r.snapshot()
	if len(got) != 3 || got[0] != "c" || got[2] != "e" {
		t.Errorf("ring = %v, want [c d e]", got)
	}
}
