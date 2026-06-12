// Package kube manages kubectl port-forward sessions: it spawns kubectl,
// watches its output and exit status, and reconnects automatically with
// exponential backoff when the connection drops.
//
// Sessions live as child processes of this program — they end when the
// program does. That makes them perfect for the TUI's lifetime and for the
// foreground `ports fwd` command, and avoids any daemon machinery.
package kube

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Status describes where a session is in its lifecycle.
type Status string

const (
	StatusConnecting   Status = "connecting"
	StatusConnected    Status = "connected"
	StatusReconnecting Status = "reconnecting"
	StatusStopped      Status = "stopped"
	StatusFailed       Status = "failed" // gave up (e.g. kubectl missing)
)

// Spec describes a port-forward to establish.
type Spec struct {
	Context   string   // --context; "" = current
	Namespace string   // -n; "" = default
	Target    string   // e.g. "pod/web-0", "svc/api", "deploy/web"
	Ports     []string // e.g. "8080:80", "5432"
}

// Validate sanity-checks a spec before spawning anything.
func (s Spec) Validate() error {
	if strings.TrimSpace(s.Target) == "" {
		return fmt.Errorf("target is required (e.g. svc/api, pod/web-0)")
	}
	if len(s.Ports) == 0 {
		return fmt.Errorf("at least one port mapping is required (e.g. 8080:80)")
	}
	for _, p := range s.Ports {
		for _, part := range strings.SplitN(p, ":", 2) {
			for _, r := range part {
				if r < '0' || r > '9' {
					return fmt.Errorf("invalid port mapping %q", p)
				}
			}
			if part == "" {
				return fmt.Errorf("invalid port mapping %q", p)
			}
		}
	}
	return nil
}

// Label renders a compact human identifier for the session.
func (s Spec) Label() string {
	l := s.Target + " " + strings.Join(s.Ports, ",")
	if s.Namespace != "" {
		l += " -n " + s.Namespace
	}
	if s.Context != "" {
		l += " @" + s.Context
	}
	return l
}

// EventKind tags session lifecycle events.
type EventKind string

const (
	EventConnected    EventKind = "connected"
	EventDisconnected EventKind = "disconnected"
	EventStopped      EventKind = "stopped"
	EventFailed       EventKind = "failed"
)

// Event is emitted on session state transitions (for UI + notifications).
type Event struct {
	SessionID string
	Kind      EventKind
	Detail    string
}

// Session is one managed kubectl port-forward.
type Session struct {
	ID   string
	Spec Spec

	mu        sync.Mutex
	status    Status
	restarts  int
	startedAt time.Time
	lastErr   string
	logs      *ring

	cancel context.CancelFunc
}

// Status returns the current lifecycle state.
func (s *Session) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Restarts returns how many times the session has reconnected.
func (s *Session) Restarts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restarts
}

// StartedAt returns when the session was created.
func (s *Session) StartedAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startedAt
}

// LastError returns the most recent failure detail, if any.
func (s *Session) LastError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

// Logs returns a copy of the session's recent output lines.
func (s *Session) Logs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logs.snapshot()
}

func (s *Session) setStatus(st Status) {
	s.mu.Lock()
	s.status = st
	s.mu.Unlock()
}

func (s *Session) appendLog(line string) {
	s.mu.Lock()
	s.logs.append(time.Now().Format("15:04:05") + "  " + line)
	s.mu.Unlock()
}

// Manager owns all sessions and the shared event stream.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	events   chan Event
	nextID   atomic.Int64

	// Kubectl is the binary to spawn; overridable for tests.
	Kubectl string
	// MaxBackoff caps the reconnect delay.
	MaxBackoff time.Duration
}

// NewManager returns a Manager with a buffered event stream.
func NewManager() *Manager {
	return &Manager{
		sessions:   map[string]*Session{},
		events:     make(chan Event, 64),
		Kubectl:    "kubectl",
		MaxBackoff: 30 * time.Second,
	}
}

// Events exposes the lifecycle event stream (UI/notification fan-in).
func (m *Manager) Events() <-chan Event { return m.events }

// List returns sessions sorted by creation order.
func (m *Manager) List() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	sortSessions(out)
	return out
}

// Get fetches one session by id.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// Start validates the spec, registers a session, and launches its supervisor
// goroutine. The returned session is already running (or connecting).
func (m *Manager) Start(spec Spec) (*Session, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if _, err := exec.LookPath(m.Kubectl); err != nil {
		return nil, fmt.Errorf("%s not found in PATH", m.Kubectl)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		ID:        fmt.Sprintf("fwd-%d", m.nextID.Add(1)),
		Spec:      spec,
		status:    StatusConnecting,
		startedAt: time.Now(),
		logs:      newRing(200),
		cancel:    cancel,
	}
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()

	go m.supervise(ctx, s)
	return s, nil
}

// Stop terminates a session's kubectl and marks it stopped.
func (m *Manager) Stop(id string) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("no such session %s", id)
	}
	s.setStatus(StatusStopped)
	s.cancel()
	m.emit(Event{SessionID: id, Kind: EventStopped, Detail: s.Spec.Label()})
	return nil
}

// Remove drops a stopped/failed session from the list.
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		s.cancel()
		delete(m.sessions, id)
	}
}

// StopAll terminates everything (program shutdown).
func (m *Manager) StopAll() {
	for _, s := range m.List() {
		if s.Status() == StatusConnected || s.Status() == StatusConnecting || s.Status() == StatusReconnecting {
			_ = m.Stop(s.ID)
		}
	}
}

// supervise runs the spawn→watch→backoff→respawn loop for one session.
func (m *Manager) supervise(ctx context.Context, s *Session) {
	backoff := time.Second
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return
		}
		if attempt > 0 {
			s.setStatus(StatusReconnecting)
			s.mu.Lock()
			s.restarts++
			s.mu.Unlock()
			s.appendLog(fmt.Sprintf("reconnecting in %s (attempt %d)", backoff, attempt))
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > m.MaxBackoff {
				backoff = m.MaxBackoff
			}
		}

		connected, err := m.runOnce(ctx, s)
		if ctx.Err() != nil {
			return // stopped deliberately
		}
		if connected {
			// A successful connection resets the backoff ladder.
			backoff = time.Second
			m.emit(Event{SessionID: s.ID, Kind: EventDisconnected, Detail: s.Spec.Label()})
		}
		if err != nil {
			s.mu.Lock()
			s.lastErr = err.Error()
			s.mu.Unlock()
			s.appendLog("error: " + err.Error())
		}
	}
}

// runOnce spawns kubectl once and blocks until it exits. Returns whether the
// forward reached the connected state during this run.
func (m *Manager) runOnce(ctx context.Context, s *Session) (connected bool, err error) {
	args := []string{"port-forward"}
	if s.Spec.Context != "" {
		args = append(args, "--context", s.Spec.Context)
	}
	if s.Spec.Namespace != "" {
		args = append(args, "-n", s.Spec.Namespace)
	}
	args = append(args, s.Spec.Target)
	args = append(args, s.Spec.Ports...)

	cmd := exec.CommandContext(ctx, m.Kubectl, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false, err
	}
	cmd.Stderr = cmd.Stdout // interleave; kubectl writes errors to stderr

	s.setStatus(StatusConnecting)
	s.appendLog("$ " + m.Kubectl + " " + strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		return false, err
	}

	sawForwarding := &atomic.Bool{}
	go m.scanOutput(stdout, s, sawForwarding)

	waitErr := cmd.Wait()
	return sawForwarding.Load(), waitErr
}

// scanOutput tails kubectl output, flipping the session to connected when
// the "Forwarding from" banner appears.
func (m *Manager) scanOutput(r io.Reader, s *Session, sawForwarding *atomic.Bool) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		s.appendLog(line)
		if strings.HasPrefix(line, "Forwarding from") && !sawForwarding.Swap(true) {
			s.setStatus(StatusConnected)
			m.emit(Event{SessionID: s.ID, Kind: EventConnected, Detail: s.Spec.Label()})
		}
	}
}

func (m *Manager) emit(e Event) {
	select {
	case m.events <- e:
	default: // never block a supervisor on a slow consumer
	}
}

func sortSessions(ss []*Session) {
	// IDs are fwd-1, fwd-2, … — lexicographic compare of the numeric suffix
	// via length-then-string ordering keeps creation order.
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && less(ss[j].ID, ss[j-1].ID); j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

func less(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

// ring is a fixed-capacity line buffer.
type ring struct {
	lines []string
	max   int
}

func newRing(max int) *ring { return &ring{max: max} }

func (r *ring) append(line string) {
	r.lines = append(r.lines, line)
	if len(r.lines) > r.max {
		r.lines = r.lines[len(r.lines)-r.max:]
	}
}

func (r *ring) snapshot() []string {
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}
