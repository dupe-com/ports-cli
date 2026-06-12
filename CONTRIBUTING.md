# Contributing to ports-cli

Thanks for taking the time! Issues and PRs are welcome.

## Development setup

```sh
git clone https://github.com/dupe-com/ports-cli
cd ports-cli
make build      # ./bin/ports
make test       # full suite (race detector on)
make lint
```

Go ≥ 1.26. No CGO, no system deps beyond what ships on the OS
(`lsof` on macOS — part of the base system).

## Project layout

| Path | What |
| --- | --- |
| `cmd/ports` | entrypoint, version ldflags |
| `internal/cli` | cobra commands (`list`, `kill`, `watch`, `fwd`) |
| `internal/tui` | bubbletea app — tabs, tables, modals |
| `internal/netscan` | listener discovery (lsof on darwin, gopsutil elsewhere) |
| `internal/proc` | SIGTERM → SIGKILL escalation |
| `internal/categorize` | port/process → category heuristics |
| `internal/config` | TOML config: favorites, watched, intervals |
| `internal/kube` | supervised auto-reconnecting `kubectl port-forward` sessions |
| `internal/cftunnel` | cloudflared process detection |
| `internal/notify` | desktop notifications per-platform |

## Guidelines

- **Tests**: anything with parsing or a state machine gets a table-driven
  test. Run `make test` before pushing; CI runs macOS + Linux with `-race`.
- **Formatting**: `gofmt` (CI enforces). `make fmt`.
- **Commits**: conventional-ish prefixes appreciated (`feat:`, `fix:`,
  `docs:`…) — the release changelog filters on them.
- **New categories/rules**: `internal/categorize` rules are deliberately
  substring-simple. Add the rule + a test case.
- **Scope**: the tool stays daemon-free. Forward sessions die with the
  process by design; anything that needs persistent background state is
  probably out of scope.

## Releasing (maintainers)

```sh
git tag v0.x.y && git push --tags
```

The release workflow runs goreleaser: cross-platform binaries, checksums,
changelog, and the Homebrew formula (when `HOMEBREW_TAP_TOKEN` is configured).
