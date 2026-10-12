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
| `internal/core` | Builds the services, and the operations every interface shares |
| `internal/cli` | Commands, and the terminal's own pieces (mpv, Chrome, Discord) |
| `internal/tui` | The full-screen interface |
| `internal/episodes` | A show's episode list: what's watched, what's next |
| `internal/session` | One watch: choosing a stream, playing it, recording progress |
| `internal/provider/*` | The sites episodes come from, one package each |
| `internal/mapping` | Matching an AniList show to a provider's own entry |
| `internal/streamproxy`, `internal/hls` | Serving streams mpv can't fetch itself |
| `internal/httpx`, `internal/browser`, `internal/sniff` | HTTP with retries, and Chrome for bot checks and embed players |
| `internal/store` | SQLite: history, list mirror, mappings, caches |
| `internal/tracker`, `internal/anilist` | The user's list, locally and on AniList |
| `internal/skip`, `internal/update`, `internal/discord` | Skip times, upgrades, presence |
| `mobile` | The core as the Android app sees it, through gomobile |
| `android/` | The Android app: Kotlin, Jetpack Compose and Media3 |

## Adding a provider

Implement `provider.Provider` in a new `internal/provider/<name>` package, register it
in `App.NewRegistry` (`internal/core/core.go`), add its name to `config.ProviderNames`
and the default order, and document it in `docs/providers/<name>.md` with a row in
[status.md](providers/status.md). Tests go against fixtures in `testdata/`, plus the
shared contract suite in `internal/provider/providertest` and a `//go:build live` test
like `internal/provider/senshi/live_test.go`.

## The Android app

The app is a Kotlin UI over the same Go core: `make android-aar` builds `mobile/` into
`android/app/libs/tsuzuki.aar` with gomobile, and the app calls it with JSON strings.
Logic both interfaces need belongs in Go (`internal/core`, `internal/episodes`), so the
two don't drift. Where the terminal uses mpv and Chrome, the app implements the core's
`Host`: ExoPlayer plays (`player.Remote` stands in for mpv's IPC), and WebViews run
embed players and bot checks.

It needs JDK 17 or newer and the Android SDK with platform 37, build-tools 37.0.0 and
NDK 30.0.16248370, with `ANDROID_HOME` and `ANDROID_NDK_HOME` set:

```sh
make android          # debug APKs in android/app/build/outputs/apk/debug
make android-install  # and install the universal one on the phone adb sees
go test -tags live -run TestLiveSearchAndWatch ./mobile   # the core's app path, live
```

Rebuild the AAR after changing `mobile/`: Gradle doesn't know about the Go side.

## Releasing

Pushing a `v*` tag publishes a GitHub release, updates Homebrew, the AUR, winget, the
apt repository and Fedora's Copr. See [releasing.md](releasing.md).
