package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tempConfig(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("PORTS_CLI_CONFIG", p)
	return p
}

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	tempConfig(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Refresh() != DefaultRefreshInterval {
		t.Errorf("Refresh() = %v, want default %v", c.Refresh(), DefaultRefreshInterval)
	}
	if c.Grace() != DefaultGracePeriod {
		t.Errorf("Grace() = %v, want default %v", c.Grace(), DefaultGracePeriod)
	}
	if !c.Notify {
		t.Error("Notify should default to true")
	}
}

func TestRoundTrip(t *testing.T) {
	p := tempConfig(t)

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c.RefreshInterval = "5s"
	c.GracePeriod = "3s"
	c.Favorites = []uint32{3000, 5432}
	c.Watched = []uint32{8080}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("config file not written: %v", err)
	}

	c2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c2.Refresh() != 5*time.Second {
		t.Errorf("Refresh() = %v after round-trip", c2.Refresh())
	}
	if c2.Grace() != 3*time.Second {
		t.Errorf("Grace() = %v after round-trip", c2.Grace())
	}
	if !c2.IsFavorite(3000) || !c2.IsFavorite(5432) || c2.IsFavorite(9999) {
		t.Errorf("favorites wrong: %v", c2.Favorites)
	}
	if !c2.IsWatched(8080) || c2.IsWatched(3000) {
		t.Errorf("watched wrong: %v", c2.Watched)
	}
}

func TestToggles(t *testing.T) {
	tempConfig(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	on, err := c.ToggleFavorite(3000)
	if err != nil || !on {
		t.Fatalf("first toggle should turn on: on=%v err=%v", on, err)
	}
	on, err = c.ToggleFavorite(3000)
	if err != nil || on {
		t.Fatalf("second toggle should turn off: on=%v err=%v", on, err)
	}

	// toggles persist
	if _, err := c.ToggleWatched(8080); err != nil {
		t.Fatal(err)
	}
	c2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c2.IsWatched(8080) {
		t.Error("watched toggle did not persist")
	}
	if c2.IsFavorite(3000) {
		t.Error("favorite double-toggle should have removed the port")
	}
}

func TestBadDurationsFallBack(t *testing.T) {
	tempConfig(t)
	c, _ := Load()
	c.RefreshInterval = "not-a-duration"
	c.GracePeriod = "-3s"
	if c.Refresh() != DefaultRefreshInterval {
		t.Errorf("bad refresh should fall back, got %v", c.Refresh())
	}
	if c.Grace() != DefaultGracePeriod {
		t.Errorf("bad grace should fall back, got %v", c.Grace())
	}
}
