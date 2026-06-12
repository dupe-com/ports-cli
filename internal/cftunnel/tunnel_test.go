package cftunnel

import "testing"

func TestParseCmdline(t *testing.T) {
	cases := []struct {
		cmdline string
		mode    Mode
		name    string
		origin  string
		config  string
		host    string
	}{
		{
			cmdline: "cloudflared tunnel run my-tunnel",
			mode:    ModeNamed, name: "my-tunnel",
		},
		{
			cmdline: "cloudflared tunnel --url http://localhost:3000",
			mode:    ModeQuick, origin: "http://localhost:3000",
		},
		{
			cmdline: "cloudflared tunnel --url=http://localhost:8080",
			mode:    ModeQuick, origin: "http://localhost:8080",
		},
		{
			cmdline: "cloudflared tunnel --config /etc/cloudflared/config.yml run",
			mode:    ModeNamed, config: "/etc/cloudflared/config.yml",
		},
		{
			cmdline: "cloudflared tunnel run --token abc123 7f3e2a",
			mode:    ModeNamed, name: "7f3e2a",
		},
		{
			cmdline: "cloudflared tunnel --hostname app.example.com --url http://localhost:5173",
			mode:    ModeQuick, origin: "http://localhost:5173", host: "app.example.com",
		},
		{
			cmdline: "cloudflared proxy-dns",
			mode:    ModeOther,
		},
	}
	for _, c := range cases {
		got := ParseCmdline(c.cmdline)
		if got.Mode != c.mode {
			t.Errorf("%q: mode = %s, want %s", c.cmdline, got.Mode, c.mode)
		}
		if got.Name != c.name {
			t.Errorf("%q: name = %q, want %q", c.cmdline, got.Name, c.name)
		}
		if got.Origin != c.origin {
			t.Errorf("%q: origin = %q, want %q", c.cmdline, got.Origin, c.origin)
		}
		if got.ConfigPath != c.config {
			t.Errorf("%q: config = %q, want %q", c.cmdline, got.ConfigPath, c.config)
		}
		if got.Hostname != c.host {
			t.Errorf("%q: hostname = %q, want %q", c.cmdline, got.Hostname, c.host)
		}
	}
}
