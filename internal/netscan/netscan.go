// Package netscan discovers TCP ports in the LISTEN state and enriches them
// with information about the owning process.
//
// On macOS it shells out to lsof (the most reliable unprivileged source of
// socket→pid mappings there); on other platforms it uses gopsutil's
// cross-platform connection table.
package netscan

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// Listener is one (port, process) pair in the LISTEN state. A process
// listening on both IPv4 and IPv6 for the same port is collapsed into a
// single Listener with both bind addresses recorded.
type Listener struct {
	Port  uint32   `json:"port"`
	Addrs []string `json:"addrs"` // bind addresses, e.g. "127.0.0.1", "*", "[::1]"
	PID   int32    `json:"pid"`

	// Process enrichment. Best-effort: system processes may deny access,
	// in which case Name carries whatever the socket table reported and
	// the rest stay zero.
	Name       string  `json:"name"`
	User       string  `json:"user"`
	Cmdline    string  `json:"cmdline"`
	StartedAt  int64   `json:"started_at_ms"` // unix ms; 0 if unknown
	CPUPercent float64 `json:"cpu_percent"`
	MemPercent float64 `json:"mem_percent"`
}

// Uptime renders the time since process start, or "" when unknown.
func (l Listener) Uptime() string {
	if l.StartedAt == 0 {
		return ""
	}
	d := time.Since(time.UnixMilli(l.StartedAt))
	if d < 0 {
		return ""
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

// AddrSummary joins bind addresses for display.
func (l Listener) AddrSummary() string { return strings.Join(l.Addrs, ",") }

// rawListener is what the platform scanners produce before enrichment.
type rawListener struct {
	port uint32
	addr string
	pid  int32
	cmd  string // process name as reported by the socket table (may be truncated)
	user string // login name if the socket table knew it
}

// Scan returns all listening TCP ports, sorted by port then pid.
func Scan() ([]Listener, error) {
	var (
		raws []rawListener
		err  error
	)
	if runtime.GOOS == "darwin" {
		raws, err = scanLsof()
	} else {
		raws, err = scanGopsutil()
	}
	if err != nil {
		return nil, err
	}
	return enrich(dedupe(raws)), nil
}

// PortsForPID lists every port a pid is listening on — used by detail views.
func PortsForPID(listeners []Listener, pid int32) []uint32 {
	var out []uint32
	for _, l := range listeners {
		if l.PID == pid {
			out = append(out, l.Port)
		}
	}
	return out
}

// FilterPorts keeps listeners bound to any of the given ports.
func FilterPorts(listeners []Listener, ports []uint32) []Listener {
	want := make(map[uint32]bool, len(ports))
	for _, p := range ports {
		want[p] = true
	}
	var out []Listener
	for _, l := range listeners {
		if want[l.Port] {
			out = append(out, l)
		}
	}
	return out
}

// FilterName keeps listeners whose process name or cmdline contains q
// (case-insensitive).
func FilterName(listeners []Listener, q string) []Listener {
	q = strings.ToLower(q)
	var out []Listener
	for _, l := range listeners {
		if strings.Contains(strings.ToLower(l.Name), q) ||
			strings.Contains(strings.ToLower(l.Cmdline), q) {
			out = append(out, l)
		}
	}
	return out
}

// dedupe collapses duplicate (pid, port) rows (e.g. v4+v6 sockets) and
// collects their distinct bind addresses.
func dedupe(raws []rawListener) []Listener {
	type key struct {
		pid  int32
		port uint32
	}
	idx := make(map[key]int)
	var out []Listener
	for _, r := range raws {
		k := key{r.pid, r.port}
		if i, ok := idx[k]; ok {
			if r.addr != "" && !contains(out[i].Addrs, r.addr) {
				out[i].Addrs = append(out[i].Addrs, r.addr)
			}
			continue
		}
		idx[k] = len(out)
		l := Listener{Port: r.port, PID: r.pid, Name: r.cmd, User: r.user}
		if r.addr != "" {
			l.Addrs = []string{r.addr}
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].PID < out[j].PID
	})
	return out
}

// enrich fills process details, querying each distinct pid once.
func enrich(ls []Listener) []Listener {
	cache := map[int32]*process.Process{}
	for i := range ls {
		p, ok := cache[ls[i].PID]
		if !ok {
			p, _ = process.NewProcess(ls[i].PID)
			cache[ls[i].PID] = p
		}
		if p == nil {
			continue
		}
		if name, err := p.Name(); err == nil && name != "" {
			ls[i].Name = name
		}
		if ls[i].User == "" {
			if u, err := p.Username(); err == nil {
				ls[i].User = u
			}
		}
		if cl, err := p.Cmdline(); err == nil && cl != "" {
			ls[i].Cmdline = cl
		}
		if ct, err := p.CreateTime(); err == nil {
			ls[i].StartedAt = ct
		}
		if cpu, err := p.CPUPercent(); err == nil {
			ls[i].CPUPercent = cpu
		}
		if mem, err := p.MemoryPercent(); err == nil {
			ls[i].MemPercent = float64(mem)
		}
		if ls[i].Cmdline == "" {
			ls[i].Cmdline = ls[i].Name
		}
	}
	return ls
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
