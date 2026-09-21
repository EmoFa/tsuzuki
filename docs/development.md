# Development

```sh
mise install      # Go, golangci-lint and goreleaser at the pinned versions
make build        # binary in bin/tsuzuki
make test         # unit tests (those needing mpv or Chromium skip without them)
make lint         # go vet and golangci-lint for linux, darwin and windows
make test-live    # tests against the real provider sites
make cross        # build every platform into dist/
make snapshot     # release archives and packages, without publishing
```

Building without mise needs Go 1.25 or newer: `go build ./cmd/tsuzuki`.

## Layout

| Package | What it does |
|---|---|
| `internal/cli` | Commands, and the wiring that builds everything else |
| `internal/tui` | The full-screen interface |
| `internal/session` | One watch: choosing a stream, playing it, recording progress |
| `internal/provider/*` | The sites episodes come from, one package each |
| `internal/mapping` | Matching an AniList show to a provider's own entry |
| `internal/streamproxy`, `internal/hls` | Serving streams mpv can't fetch itself |
| `internal/httpx`, `internal/browser` | HTTP with retries, and Chrome for bot checks |
| `internal/store` | SQLite: history, list mirror, mappings, caches |
| `internal/tracker`, `internal/anilist` | The user's list, locally and on AniList |
| `internal/skip`, `internal/update`, `internal/discord` | Skip times, upgrades, presence |

## Adding a provider

Implement `provider.Provider` in a new `internal/provider/<name>` package, register it
in `App.newRegistry` (`internal/cli/services.go`), add its name to `config.ProviderNames`
and the default order, and document it in `docs/providers/<name>.md` with a row in
[status.md](providers/status.md). Tests go against fixtures in `testdata/`, plus the
shared contract suite in `internal/provider/providertest` and a `//go:build live` test
like `internal/provider/senshi/live_test.go`.

## Releasing

Pushing a `v*` tag publishes a GitHub release, updates Homebrew, the AUR, winget, the
apt repository and Fedora's Copr. See [releasing.md](releasing.md).
