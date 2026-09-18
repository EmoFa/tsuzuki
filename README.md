<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo.png">
    <img src="docs/assets/logo-light.png" alt="tsuzuki" width="480">
  </picture>
</p>

Watch anime from your terminal. Find a show, pick an episode, and tsuzuki plays it
in [mpv](https://mpv.io), remembering where you stopped and keeping your
[AniList](https://anilist.co) up to date.

- **Several providers with fallback.** Anikoto, Senshi, Animepahe and KickAssAnime are
  tried in the order you choose, up to 1080p. A stream that doesn't work, or
  stops partway, moves on to the next provider at the same position.
- **Gets past Cloudflare and DDoS-Guard** with a built-in Chrome/Chromium session.
- **Continue where you left off:** per-episode history, resume, and autoplay. Rewatch
  a show and progress counts from episode 1 again without losing the first watch.
- **Browse** this season, trending, popular, top-rated and upcoming anime, filtered by
  genre and format.
- **AniList tracking** with a browser login, or fully local tracking. Finish a show
  and tsuzuki asks for your score, then offers the sequel.
- **Skip openings, endings and recaps** automatically or at a key press, and
  optionally skip whole filler and recap episodes.
- **Discord Rich Presence** with the title, episode, cover and progress.
- A full-screen terminal UI, plain commands for scripting, and a
  [config file](docs/config.md) for everything else.

tsuzuki doesn't host anything: it finds streams on third-party sites and plays them.

## Requirements

- **mpv** for playback. tsuzuki finds it on your `PATH` and in the usual install
  locations. On Windows that includes the folder next to `tsuzuki.exe` and an mpv
  registered with `mpv --register`; otherwise set `player.mpv_path`.
- **Chrome, Chromium or Edge** for Anikoto and for bot-protection checks. Without one,
  set `browser.auto_download = true` to have tsuzuki fetch Chromium, or remove
  Anikoto from `providers.order`.
- Linux, macOS or Windows.

## Install

| Platform | Command |
|---|---|
| macOS, Linux ([Homebrew](https://brew.sh)) | `brew install EmoFa/tap/tsuzuki` |
| Windows | `winget install EmoFa.tsuzuki` |

Each installs mpv too, and updates with your package manager (`brew upgrade`,
`winget upgrade`). On Windows, open a new terminal after installing
so `tsuzuki` is on your `PATH`.

Archives, `.deb` and `.rpm` packages are also on the
[releases page](https://github.com/EmoFa/tsuzuki/releases).

**With Go** (1.27 or newer):

```sh
go install github.com/EmoFa/tsuzuki/cmd/tsuzuki@latest
```

**From source:**

```sh
git clone https://github.com/EmoFa/tsuzuki && cd tsuzuki
make build        # binary in bin/tsuzuki
```

Then check your setup:

```sh
tsuzuki doctor
```

## Getting started

Run `tsuzuki` to open the terminal UI. Press <kbd>/</kbd> to search, pick a show,
and press <kbd>Enter</kbd> on an episode. Next time, your shows are on the home
screen under **Continue watching**.

To keep AniList in sync, log in once:

```sh
tsuzuki login      # opens AniList in your browser
```

From then on, each episode you finish (85% by default) is recorded on your AniList
list, and the list tabs in the UI mirror it. Changes made while AniList is
unreachable are sent later. Set `tracking.backend = "local"` to keep everything
on your machine.

### Keys

| Where | Key | Action |
|---|---|---|
| Everywhere | <kbd>?</kbd> | Help |
| | <kbd>Esc</kbd> | Back |
| | <kbd>Ctrl</kbd>+<kbd>C</kbd> | Quit |
| Home | <kbd>/</kbd> | Search |
| | <kbd>b</kbd> | Browse: this season, trending, popular, top rated, next season |
| | <kbd>←</kbd> <kbd>→</kbd> | Continue watching, and your list tabs |
| | <kbd>Enter</kbd> | Continue the selected show |
| | <kbd>d</kbd> | Show details |
| | <kbd>,</kbd> | Settings (where <kbd>L</kbd> logs in and <kbd>S</kbd> syncs) |
| Show details | <kbd>Enter</kbd> | Play the selected episode |
| | <kbd>c</kbd> | Continue from the next unwatched episode |
| | <kbd>#</kbd> | Jump to an episode number |
| | <kbd>w</kbd> / <kbd>W</kbd> | Mark the episode watched (toggle) / everything up to it |
| | <kbd>m</kbd> | Switch between sub and dub |
| | <kbd>l</kbd> | Change the list status |
| | <kbd>s</kbd> | Subtitle languages for this show |
| | <kbd>r</kbd> | Open the sequel |
| | <kbd>R</kbd> | Rewatch from episode 1 (history kept) |
| Browse | <kbd>←</kbd> <kbd>→</kbd> | Switch list |
| | <kbd>f</kbd> | Filter by genre and format |
| Finished a show | <kbd>1</kbd>–<kbd>9</kbd>, <kbd>0</kbd> | Rate it (0 = 10), or <kbd>s</kbd> to skip |
| | <kbd>Enter</kbd> / <kbd>p</kbd> | Watch the sequel / add it to Planning |
| Now playing | <kbd>n</kbd> / <kbd>p</kbd> | Next / previous episode |
| | <kbd>f</kbd> | Try another provider |
| | <kbd>x</kbd> | Stop |
| In mpv | <kbd>Tab</kbd> | Skip the current opening, ending or recap (when prompted) |

Seeking, pausing and everything else use mpv's own keys.

### Commands

The UI isn't required: every feature has a command.

```sh
tsuzuki search frieren             # find the AniList ID
tsuzuki discover trending --genre Action  # season, trending, popular, top, upcoming
tsuzuki watch 154587               # resume, or start from episode 1
tsuzuki watch 154587 5 --mode dub  # a specific episode
tsuzuki watch 154587 --subs es,en  # subtitle languages for this watch ("off" hides them)
tsuzuki continue                   # the show you watched last
tsuzuki history                    # recently watched
tsuzuki list watching              # your list (watching, planning, completed, ...)
tsuzuki rate 154587 9              # score a show from 1 to 10
tsuzuki rewatch 154587             # start again from episode 1, keeping history
tsuzuki mark 1735 57-71            # mark episodes watched (--unwatched to undo)
tsuzuki login | logout | whoami | sync
tsuzuki config init | edit | check | path
tsuzuki doctor [--streams]         # check dependencies, services and providers
```

## Configuration

`tsuzuki config init` writes a commented config file with every setting and its
default. [docs/config.md](docs/config.md) describes each one: provider order,
quality, autoplay, skipping, tracking, Discord, and where tsuzuki keeps its files.

## Troubleshooting

- **Start with `tsuzuki doctor`.** It checks mpv, the browser, Discord, your AniList
  login, AniSkip and the filler list, and searches every provider. Add `--streams` to
  also test playback from each provider.
- **Logs** are in the cache directory (`tsuzuki config path` shows it). Run with
  `--debug` for more detail.
- **A verification window opened.** A site's bot check needs a human. Complete it
  once, and tsuzuki reuses the result for as long as the site accepts it.
- **The wrong show plays, or a provider can't find one it has.** See what tsuzuki
  matched with `tsuzuki debug resolve <anilist-id>`. Pin the right show with
  `tsuzuki debug map <anilist-id> <provider> <show-id>` (find show IDs with
  `tsuzuki debug search <provider> <title>`), or forget a match with
  `tsuzuki debug unmap <anilist-id>`.
- **Verification keeps failing.** `tsuzuki debug clearances` lists what's stored,
  and `tsuzuki debug reset-browser` starts over with a fresh browser profile.

Provider notes and current status: [docs/providers](docs/providers/status.md).

## What tsuzuki sends where

- **AniList:** searches, show details, and (when logged in) your list and progress.
- **AniSkip and Anime Filler List:** the MAL ID and episode number, or show title,
  of what you watch, to look up skip times and filler episodes.
- **Providers:** searches and episode requests, as a browser would.
- **Discord:** what you're watching, through the Discord app on your computer
  (`discord.enabled = false` turns it off).
- **GitHub:** at most once a day, a request for the latest release number, to tell
  you about updates (`general.check_updates = false` turns it off).

Nothing else leaves your machine. There is no telemetry.

## Development

```sh
mise install      # Go, golangci-lint and goreleaser at the pinned versions
make test         # unit tests (tests needing mpv or Chromium skip without them)
make lint         # go vet and golangci-lint for linux, darwin and windows
make test-live    # tests against the real sites
make cross        # build every platform into dist/
make snapshot     # release archives and packages, without publishing
```

Pushing a `v*` tag publishes a GitHub release with goreleaser, updates the
Homebrew tap and (once set up) the AUR package, and opens a winget pull request. See
[docs/releasing.md](docs/releasing.md).

## License

[MIT](LICENSE)
