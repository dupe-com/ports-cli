<h1 align="center">ports</h1>

<p align="center">
  <b>See what's listening. Reclaim the port. One keypress.</b><br>
  An interactive TCP port manager for your terminal — with managed
  <code>kubectl port-forward</code> sessions and Cloudflare Tunnel visibility.
</p>

<p align="center">
  <a href="https://github.com/dupe-com/ports-cli/actions/workflows/ci.yml"><img src="https://github.com/dupe-com/ports-cli/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://goreportcard.com/report/github.com/dupe-com/ports-cli"><img src="https://goreportcard.com/badge/github.com/dupe-com/ports-cli" alt="Go Report Card"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT"></a>
</p>

---

You run `bun dev` and ports 3000, 3001, and 8484 are "in use". By what?
An orphaned SSH tunnel? Yesterday's dev server? `ports` answers in one
screen and clears it in one keypress.

```
 1 Ports    2 Forwards    3 Tunnels

   PORT    CAT  PID      USER     UPTIME  ADDR      COMMAND
  ▸3000    DEV  7881     ramin    17h     *         node — next dev
  ★5432    DB   801      ramin    3d2h    127.0.0.1 postgres -D /opt/homebrew/var…
   8080    WEB  81261    ramin    2h      *         mitmweb 👁
   11434   DEV  39156    ramin    1d4h    127.0.0.1 ollama serve

  / filter · space sel · enter kill · d detail · f fav · w watch · c cat · r refresh
```

## Features

- ⚡ **Live table of every listening TCP port** — process, owner, uptime,
  CPU/mem, bind address. Auto-refreshes (configurable, pausable).
- 🔪 **One-keypress kill** — graceful `SIGTERM` with a grace window, `F` to
  escalate to `SIGKILL`. Multi-select with `space` to clear several at once.
- 🔍 **Fuzzy filter** (`/`) across port, process name, user, and full command
  line — plus a category filter (`c`) that cycles dev / web / db / messaging /
  system.
- 🏷️ **Smart categorization** — postgres on a weird port is still a `DB`;
  rules match the process first, well-known ports second.
- ★ **Favorites** — pin the ports you care about to the top.
- 👁️ **Watched ports** — get a desktop notification when a port starts or
  stops listening ("tell me when the dev server is actually up").
- ☸️ **Managed `kubectl port-forward` sessions** — create from a form, watch
  status live, view logs, and let them **auto-reconnect with backoff** when
  the connection drops. No more dead forwards after a pod restart.
- ☁️ **Cloudflare Tunnel visibility** — see every running `cloudflared`,
  named or quick, with its origin and config.
- 🤖 **Scriptable** — every feature has a flag-driven subcommand with
  `--json` output where it matters.

## Install

```sh
# Homebrew (macOS / Linux)
brew install dupe-com/tap/ports-cli

# Go
go install github.com/dupe-com/ports-cli/cmd/ports@latest

# curl (downloads the right release binary to /usr/local/bin)
curl -fsSL https://raw.githubusercontent.com/dupe-com/ports-cli/main/install.sh | sh
```

Or grab a binary from [Releases](https://github.com/dupe-com/ports-cli/releases).

## Usage

### TUI

```sh
ports
```

| Key | Action |
| --- | --- |
| `1` `2` `3` / `tab` | switch tabs (Ports / Forwards / Tunnels) |
| `↑↓` `j` `k` | move · `g`/`G` top/bottom |
| `/` | fuzzy filter (esc clears) |
| `c` | cycle category filter |
| `space` | multi-select |
| `enter` / `x` | kill — confirm with `y` (graceful) or `F` (force) |
| `d` | detail pane (full cmdline, all ports held by the pid) |
| `f` / `w` | toggle favorite ★ / watched 👁 |
| `r` / `p` | refresh now / pause auto-refresh |
| `n` | (Forwards) new kubectl port-forward |
| `?` | help |

### CLI

```sh
ports list                        # table of all listeners
ports list --json                 # same, as JSON
ports list --category db          # only databases
ports list --filter node          # name/cmdline substring

ports kill 3000                   # kill whatever holds :3000 (confirms)
ports kill 3000 8080 --yes        # no confirmation
ports kill node --force           # by name, SIGTERM → SIGKILL

ports watch 3000                  # notify when :3000 starts/stops listening
ports watch 3000 5432 --interval 1s

ports fwd svc/api 8080:80         # kubectl port-forward that auto-reconnects
ports fwd pod/web-0 3000 -n staging --context prod
```

## Configuration

`~/.config/ports-cli/config.toml` (created on first favorite/watch; all keys
optional):

```toml
refresh_interval = "2s"     # TUI auto-refresh; "0" disables
grace_period = "1500ms"     # SIGTERM → SIGKILL window
notify = true               # desktop notifications
favorites = [3000, 5432]
watched = [8080]
```

Override the location with `$PORTS_CLI_CONFIG`.

## How it works

- **Discovery** — `lsof` field-output on macOS (the most reliable
  unprivileged source there), gopsutil's connection table elsewhere. You see
  the processes your user can see; run with `sudo` to see everything.
- **Kill** — `SIGTERM`, a grace window for clean shutdown, then opt-in
  `SIGKILL` for survivors. Multi-port processes are signalled once.
- **Forward sessions** — children of the TUI/CLI process, supervised with
  exponential backoff (1s → 30s cap), reset on successful reconnect. They end
  when `ports` does — no daemons, no state files.
- **Tunnels** — read-only detection of `cloudflared` processes via the
  process table.

## Development

```sh
make build      # build ./bin/ports
make test       # go test ./...
make lint       # golangci-lint (or go vet fallback)
make snapshot   # goreleaser snapshot build
```

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE) © Dupe, Inc.
