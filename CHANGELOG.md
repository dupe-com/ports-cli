# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
adheres to [Semantic Versioning](https://semver.org/).

## [0.2.0] - 2026-06-12

### Added
- **Focus mode (default)**: the Ports tab groups rows by category (dev
  servers first) under section headers, and folds system daemons /
  unclassified ports away. Reveal by scrolling past the end or with `a`;
  favorites, watched ports, and selections always stay visible.
- **SSH carrier awareness**: `ssh`/`autossh` listeners classify by *port*
  (an `ssh -L` of your dev server is `DEV`, shown as `DEV (SSH)` in a
  distinct tint), and unmatched forwarded ports get a new always-visible
  `TUN · tunnels & forwards` category alongside `cloudflared`.
- Tree view (`t`): group ports by owning process.
- `K` kill-everything-visible, `y` copy `localhost:PORT` to clipboard.
- Saved kubectl port-forward specs (`s` to save, `enter` to relaunch, `D`
  to delete) persisted in config.
- `esc` backs out one layer at a time (filter → fold → quit); `←`/`→`
  switch tabs; detail pane is shown by default.
- Dev port rules for Inngest (8288) and Sanity Studio (3333).

### Changed
- Tabs renamed to **Ports / kubectl / cloudflared** to make each tab's
  scope unambiguous; empty states explain observation vs management.
- Uniform tab styling with underlines; dormant tabs are dimmed.

### Fixed
- Status bar no longer truncated after a few hints (ANSI-aware width).
- Spacebar multi-select (the key arrives as a literal `" "`).
- Homebrew cask pointed at a binary named `ports-cli`; the archive ships
  `ports` (fixed in v0.1.1).

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
