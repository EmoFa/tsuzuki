# anitui

Watch anime from your terminal. Find a show, pick an episode, and anitui plays it
in [mpv](https://mpv.io), remembering where you stopped and keeping your
[AniList](https://anilist.co) up to date.

- **Several providers with fallback.** Anikoto, Senshi, AllAnime and Animepahe are
  tried in the order you choose, up to 1080p. A stream that doesn't work, or
  stops partway, moves on to the next provider at the same position.
- **Gets past Cloudflare and DDoS-Guard** with a built-in Chrome/Chromium session.
- **Continue where you left off:** per-episode history, resume, and autoplay.
- **AniList tracking** with a browser login, or fully local tracking.
- **Skip openings, endings and recaps** automatically or at a key press, and
  optionally skip whole filler and recap episodes.
- **Discord Rich Presence** with the title, episode, cover and progress.
- A full-screen terminal UI, plain commands for scripting, and a
  [config file](docs/config.md) for everything else.

anitui doesn't host anything: it finds streams on third-party sites and plays them.

## Requirements

- **mpv** for playback. anitui finds it on your `PATH` (or set `player.mpv_path`).
- **Chrome, Chromium or Edge** for Anikoto and for bot-protection checks. Without one,
  set `browser.auto_download = true` to have anitui fetch Chromium, or remove
  Anikoto from `providers.order`.
- Linux, macOS or Windows.

## Install

**Release builds.** Download an archive for your platform from the
[releases page](https://github.com/EmoFa/anitui/releases), extract it and put
`anitui` on your `PATH`. `.deb`, `.rpm` and Arch Linux packages are there too.

**With Go** (1.27 or newer):

```sh
go install github.com/EmoFa/anitui/cmd/anitui@latest
```

**From source:**

```sh
git clone https://github.com/EmoFa/anitui && cd anitui
make build        # binary in bin/anitui
```

Then check your setup:

```sh
anitui doctor
```

## Getting started

Run `anitui` to open the terminal UI. Press <kbd>/</kbd> to search, pick a show,
and press <kbd>Enter</kbd> on an episode. Next time, your shows are on the home
screen under **Continue watching**.

To keep AniList in sync, log in once:

```sh
anitui login      # opens AniList in your browser
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
| | <kbd>←</kbd> <kbd>→</kbd> | Continue watching, and your list tabs |
| | <kbd>Enter</kbd> | Continue the selected show |
| | <kbd>d</kbd> | Show details |
| | <kbd>,</kbd> | Settings (where <kbd>L</kbd> logs in and <kbd>S</kbd> syncs) |
| Show details | <kbd>Enter</kbd> | Play the selected episode |
| | <kbd>c</kbd> | Continue from the next unwatched episode |
| | <kbd>m</kbd> | Switch between sub and dub |
| | <kbd>l</kbd> | Change the list status |
| Now playing | <kbd>n</kbd> / <kbd>p</kbd> | Next / previous episode |
| | <kbd>f</kbd> | Try another provider |
| | <kbd>x</kbd> | Stop |
| In mpv | <kbd>Tab</kbd> | Skip the current opening, ending or recap (when prompted) |

Seeking, pausing and everything else use mpv's own keys.

### Commands

The UI isn't required: every feature has a command.

```sh
anitui search frieren             # find the AniList ID
anitui watch 154587               # resume, or start from episode 1
anitui watch 154587 5 --mode dub  # a specific episode
anitui continue                   # the show you watched last
anitui history                    # recently watched
anitui list watching              # your list (watching, planning, completed, ...)
anitui login | logout | whoami | sync
anitui config init | edit | check | path
anitui doctor [--streams]         # check dependencies, services and providers
```

## Configuration

`anitui config init` writes a commented config file with every setting and its
default. [docs/config.md](docs/config.md) describes each one: provider order,
quality, autoplay, skipping, tracking, Discord, and where anitui keeps its files.

## Troubleshooting

- **Start with `anitui doctor`.** It checks mpv, the browser, Discord, your AniList
  login, AniSkip and the filler list, and searches every provider. Add `--streams` to
  also test playback from each provider.
- **Logs** are in the cache directory (`anitui config path` shows it). Run with
  `--debug` for more detail.
- **A verification window opened.** A site's bot check needs a human. Complete it
  once, and anitui reuses the result for as long as the site accepts it.
- **The wrong show plays, or a provider can't find one it has.** See what anitui
  matched with `anitui debug resolve <anilist-id>`. Pin the right show with
  `anitui debug map <anilist-id> <provider> <show-id>` (find show IDs with
  `anitui debug search <provider> <title>`), or forget a match with
  `anitui debug unmap <anilist-id>`.
- **Verification keeps failing.** `anitui debug clearances` lists what's stored,
  and `anitui debug reset-browser` starts over with a fresh browser profile.

Provider notes and current status: [docs/providers](docs/providers/status.md).

## What anitui sends where

- **AniList:** searches, show details, and (when logged in) your list and progress.
- **AniSkip and Anime Filler List:** the MAL ID and episode number, or show title,
  of what you watch, to look up skip times and filler episodes.
- **Providers:** searches and episode requests, as a browser would.
- **Discord:** what you're watching, through the Discord app on your computer
  (`discord.enabled = false` turns it off).

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

Pushing a `v*` tag builds a draft GitHub release with goreleaser.
