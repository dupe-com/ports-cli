// Package config persists user preferences — favorites, watched ports, and
// behavior knobs — to a TOML file under the platform config directory
// (~/.config/ports-cli/config.toml on macOS/Linux).
package config

import (
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the on-disk shape. Zero values are replaced by defaults on Load.
type Config struct {
	// RefreshInterval is the TUI auto-refresh cadence, e.g. "2s". "0" disables.
	RefreshInterval string `toml:"refresh_interval"`
	// GracePeriod is how long SIGTERM gets before SIGKILL, e.g. "1500ms".
	GracePeriod string `toml:"grace_period"`
	// Notify enables desktop notifications for watched-port changes and
	// port-forward connect/disconnect events.
	Notify bool `toml:"notify"`
	// Favorites float to the top of the table, marked with a star.
	Favorites []uint32 `toml:"favorites"`
	// Watched ports fire a notification when they start/stop listening.
	Watched []uint32 `toml:"watched"`

	path string `toml:"-"`
}

// Defaults applied when a field is unset.
const (
	DefaultRefreshInterval = 2 * time.Second
	DefaultGracePeriod     = 1500 * time.Millisecond
)

// Path returns the config file location, honoring $PORTS_CLI_CONFIG for
// overrides (handy in tests and CI).
func Path() (string, error) {
	if p := os.Getenv("PORTS_CLI_CONFIG"); p != "" {
		return p, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ports-cli", "config.toml"), nil
}

// Load reads the config file; a missing file yields defaults, not an error.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	c := &Config{Notify: true, path: p}
	if _, err := toml.DecodeFile(p, c); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	c.path = p
	return c, nil
}

// Save writes the config atomically (temp file + rename).
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".config-*.toml")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after successful rename
	if err := toml.NewEncoder(tmp).Encode(c); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), c.path)
}

// Refresh returns the parsed refresh interval (0 = auto-refresh disabled).
func (c *Config) Refresh() time.Duration {
	if c.RefreshInterval == "" {
		return DefaultRefreshInterval
	}
	d, err := time.ParseDuration(c.RefreshInterval)
	if err != nil || d < 0 {
		return DefaultRefreshInterval
	}
	return d
}

// Grace returns the parsed SIGTERM grace window.
func (c *Config) Grace() time.Duration {
	if c.GracePeriod == "" {
		return DefaultGracePeriod
	}
	d, err := time.ParseDuration(c.GracePeriod)
	if err != nil || d <= 0 {
		return DefaultGracePeriod
	}
	return d
}

// IsFavorite reports whether port is pinned.
func (c *Config) IsFavorite(port uint32) bool { return containsPort(c.Favorites, port) }

// IsWatched reports whether port is being watched.
func (c *Config) IsWatched(port uint32) bool { return containsPort(c.Watched, port) }

// ToggleFavorite flips favorite status and persists. Returns the new state.
func (c *Config) ToggleFavorite(port uint32) (bool, error) {
	var on bool
	c.Favorites, on = togglePort(c.Favorites, port)
	return on, c.Save()
}

// ToggleWatched flips watched status and persists. Returns the new state.
func (c *Config) ToggleWatched(port uint32) (bool, error) {
	var on bool
	c.Watched, on = togglePort(c.Watched, port)
	return on, c.Save()
}

func containsPort(ps []uint32, p uint32) bool {
	for _, v := range ps {
		if v == p {
			return true
		}
	}
	return false
}

func togglePort(ps []uint32, p uint32) ([]uint32, bool) {
	for i, v := range ps {
		if v == p {
			return append(ps[:i], ps[i+1:]...), false
		}
	}
	return append(ps, p), true
}
