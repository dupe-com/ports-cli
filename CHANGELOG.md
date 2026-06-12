# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
adheres to [Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-06-12

### Added
- Initial release: interactive TUI (live port table, fuzzy filter, category
  filter, favorites, watched-port notifications, multi-select graceful/force
  kill, detail pane).
- Managed `kubectl port-forward` sessions with auto-reconnect, status, and
  logs (TUI Forwards tab + `ports fwd`).
- Cloudflare Tunnel (`cloudflared`) detection tab.
- Scriptable CLI: `ports list [--json|--category|--filter]`,
  `ports kill [--force|--yes]`, `ports watch`, `ports fwd`.
- TOML config with favorites, watched ports, refresh interval, grace period.
- Desktop notifications (macOS / Linux / Windows best-effort).
