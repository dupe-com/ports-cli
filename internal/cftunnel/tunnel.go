// Package cftunnel detects running Cloudflare Tunnel (cloudflared) processes
// and extracts what they're doing from their command lines — named tunnels,
// quick tunnels, and the local origins they expose.
package cftunnel

import (
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// Mode distinguishes the two ways cloudflared runs.
type Mode string

const (
	ModeNamed Mode = "named" // `cloudflared tunnel run <name>` (dashboard/config tunnels)
	ModeQuick Mode = "quick" // `cloudflared tunnel --url <origin>` (ephemeral trycloudflare.com)
	ModeOther Mode = "other" // anything else (access, proxy-dns, …)
)

// Tunnel is one detected cloudflared process.
type Tunnel struct {
	PID        int32  `json:"pid"`
	Mode       Mode   `json:"mode"`
	Name       string `json:"name,omitempty"`     // named tunnels: tunnel name or UUID
	Origin     string `json:"origin,omitempty"`   // quick tunnels: the --url origin
	ConfigPath string `json:"config,omitempty"`   // --config path if present
	Hostname   string `json:"hostname,omitempty"` // --hostname if present
	StartedAt  int64  `json:"started_at_ms"`
	Cmdline    string `json:"cmdline"`
}

// Uptime renders time since process start, or "".
func (t Tunnel) Uptime() string {
	if t.StartedAt == 0 {
		return ""
	}
	d := time.Since(time.UnixMilli(t.StartedAt))
	if d < 0 {
		return ""
	}
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return time.Duration(d.Round(time.Minute)).String()
	default:
		return d.Round(time.Hour).String()
	}
}

// Detect scans the process table for cloudflared instances.
func Detect() ([]Tunnel, error) {
	procs, err := process.Processes()
	if err != nil {
		return nil, err
	}
	var out []Tunnel
	for _, p := range procs {
		name, err := p.Name()
		if err != nil || !strings.Contains(strings.ToLower(name), "cloudflared") {
			continue
		}
		cmdline, _ := p.Cmdline()
		t := ParseCmdline(cmdline)
		t.PID = p.Pid
		if ct, err := p.CreateTime(); err == nil {
			t.StartedAt = ct
		}
		out = append(out, t)
	}
	return out, nil
}

// ParseCmdline classifies a cloudflared invocation. Exported for tests.
func ParseCmdline(cmdline string) Tunnel {
	t := Tunnel{Mode: ModeOther, Cmdline: cmdline}
	args := strings.Fields(cmdline)

	tunnelCmd := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		switch {
		case a == "tunnel":
			tunnelCmd = true
		case a == "run" && tunnelCmd:
			t.Mode = ModeNamed
			// the name/UUID is the first positional arg after `run` —
			// skipping flags AND their values (`--token abc` consumes two)
			for j := i + 1; j < len(args); j++ {
				if strings.HasPrefix(args[j], "-") {
					if !strings.Contains(args[j], "=") {
						j++ // value-taking flag: skip its argument too
					}
					continue
				}
				t.Name = args[j]
				break
			}
		case a == "--url" || strings.HasPrefix(a, "--url="):
			if v, ok := strings.CutPrefix(a, "--url="); ok {
				t.Origin = v
			} else {
				t.Origin = next()
			}
			if tunnelCmd && t.Mode != ModeNamed {
				t.Mode = ModeQuick
			}
		case a == "--config" || strings.HasPrefix(a, "--config="):
			if v, ok := strings.CutPrefix(a, "--config="); ok {
				t.ConfigPath = v
			} else {
				t.ConfigPath = next()
			}
		case a == "--hostname" || strings.HasPrefix(a, "--hostname="):
			if v, ok := strings.CutPrefix(a, "--hostname="); ok {
				t.Hostname = v
			} else {
				t.Hostname = next()
			}
		}
	}

	// `cloudflared tunnel --config x run` with no explicit name still counts
	// as a named tunnel (name comes from the config file).
	if tunnelCmd && t.Mode == ModeOther && t.ConfigPath != "" {
		t.Mode = ModeNamed
	}
	return t
}
