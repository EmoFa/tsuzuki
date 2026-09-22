<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo.png">
    <img src="docs/assets/logo-light.png" alt="tsuzuki" width="480">
  </picture>
</p>

Watch anime from your terminal. Find a show, pick an episode, and tsuzuki plays it
in [mpv](https://mpv.io), remembering where you stopped and keeping your
[AniList](https://anilist.co) up to date.

- **Four providers, tried in turn.** Up to 1080p; a stream that fails, or stops
  partway, moves to the next provider at the same position.
- **Continue where you left off:** per-episode history, resume and autoplay, or start a
  show again with a rewatch.
- **Skip openings, endings and recaps**, and optionally whole filler episodes.
- **Browse** this season, trending, popular, top rated and upcoming anime.
- **AniList tracking**, or keep everything on your machine. Finish a show and tsuzuki
  asks for your score, then offers the sequel.
- **Discord Rich Presence**, and a command for every feature if you'd rather script it.

tsuzuki doesn't host anything: it finds streams on third-party sites and plays them.

## Install

| Platform | Install |
|---|---|
| macOS, Linux ([Homebrew](https://brew.sh)) | `brew install EmoFa/tap/tsuzuki` |
| Debian, Ubuntu | `curl -fsSLO https://emofa.github.io/tsuzuki/tsuzuki-archive-keyring.deb && sudo apt install ./tsuzuki-archive-keyring.deb && sudo apt update && sudo apt install tsuzuki` |
| Fedora | `sudo dnf copr enable emofa/tsuzuki && sudo dnf install tsuzuki` |
| Windows ([winget](https://learn.microsoft.com/windows/package-manager/)) | `winget install EmoFa.tsuzuki` |
| Windows ([Scoop](https://scoop.sh)) | `scoop bucket add emofa https://github.com/EmoFa/scoop-bucket && scoop install tsuzuki` |
| Windows (installer) | `tsuzuki_<version>_windows_setup.exe` from the [releases page](https://github.com/EmoFa/tsuzuki/releases/latest) |
| Anywhere, with Go 1.25+ | `go install github.com/EmoFa/tsuzuki/cmd/tsuzuki@latest` |
| Anywhere, by hand | An [archive](https://github.com/EmoFa/tsuzuki/releases/latest) for your platform, kept current with `tsuzuki upgrade` |

A few things worth knowing:

- **mpv** comes with the Homebrew, apt, dnf and winget installs, and the Windows
  installer offers to add it. Otherwise install it yourself:
  `scoop install extras/mpv`, `winget install shinchiro.mpv`, or [mpv.io](https://mpv.io).
- **On Windows**, open a new terminal after installing so `tsuzuki` is on your `PATH`. The
  installer isn't signed, so Windows asks you to confirm it the first time.
- **Chrome, Chromium or Edge** is needed by one provider and by the bot checks a few sites
  use. Without one, `browser.auto_download = true` fetches a copy of Chromium.
- **`tsuzuki doctor`** checks all of the above and tells you what's missing.

## Getting started

Run `tsuzuki` for the full-screen interface. Press <kbd>/</kbd> to search, pick a show,
and press <kbd>Enter</kbd> on an episode. Next time it's waiting under **Continue
watching** on the home screen.

To keep AniList in sync, log in once with `tsuzuki login`. From then on, each episode
you finish is recorded there, the list tabs mirror it, and what your list already says
counts here: a show you're 12 episodes into shows the first 12 as watched, wherever you
watched them. Changes made while AniList is unreachable are sent later. To keep
everything local instead, set `tracking.backend = "local"`.

### Keys

Press <kbd>?</kbd> on any screen for its keys. The ones worth knowing:

| Where | Key | Action |
|---|---|---|
| Home | <kbd>/</kbd> | Search |
| | <kbd>b</kbd> | Browse this season, trending, popular, top rated, upcoming |
| | <kbd>←</kbd> <kbd>→</kbd> | Continue watching, and your list tabs |
| | <kbd>Enter</kbd> / <kbd>d</kbd> | Continue the show / show its details |
| | <kbd>,</kbd> | Settings, where you log in and sync |
| Show details | <kbd>Enter</kbd> / <kbd>c</kbd> | Play the selected episode / continue |
| | <kbd>#</kbd> | Jump to an episode number |
| | <kbd>w</kbd> / <kbd>W</kbd> | Mark the episode watched / everything up to it |
| | <kbd>m</kbd> / <kbd>l</kbd> / <kbd>s</kbd> | Sub or dub / list status / subtitles |
| | <kbd>r</kbd> / <kbd>p</kbd> / <kbd>R</kbd> | Open the sequel / the prequel / rewatch |
| Now playing | <kbd>n</kbd> / <kbd>p</kbd> / <kbd>f</kbd> | Next / previous episode / another provider |
| In mpv | <kbd>Tab</kbd> | Skip the opening, ending or recap when prompted |

Seeking, pausing and everything else use mpv's own keys.

### Commands

Every feature has a command, for scripting or for skipping the interface:

```sh
tsuzuki search frieren             # find a show's AniList ID
tsuzuki watch 154587               # resume, or start from episode 1
tsuzuki watch 154587 5 --mode dub  # a specific episode
tsuzuki continue                   # the show you watched last
tsuzuki discover trending          # season, trending, popular, top, upcoming
tsuzuki list watching              # your list (watching, planning, completed, ...)
tsuzuki history                    # recently watched
tsuzuki rate 154587 9              # score a show out of 10
tsuzuki rewatch 154587             # start again from episode 1, keeping history
tsuzuki mark 1735 57-71            # mark episodes watched (--unwatched undoes it)
tsuzuki login | logout | whoami | sync
tsuzuki config init | edit | check | path
tsuzuki doctor | upgrade
```

`tsuzuki <command> --help` describes the rest.

## Configuration

`tsuzuki config init` writes a commented file with every setting and its default:
providers and their order, quality, autoplay, skipping, subtitles, tracking and Discord.
[docs/config.md](docs/config.md) explains each one, and `tsuzuki config path` says where
your files live.

## Troubleshooting

- **Start with `tsuzuki doctor`.** It checks mpv, the browser, Discord, your AniList
  login and every provider. `--streams` also tests playback.
- **A verification window opened.** A site's bot check needs a human; complete it once
  and tsuzuki reuses the result. If it keeps reappearing, `tsuzuki debug reset-browser`
  starts fresh.
- **The wrong show plays.** `tsuzuki debug resolve <id>` shows what was matched, and
  `tsuzuki debug map <id> <provider> <show-id>` pins the right one.
- **Something else.** Logs are in the cache directory (`tsuzuki config path`); `--debug`
  records more. Provider status is tracked in [docs/providers](docs/providers/status.md).

## What tsuzuki sends where

- **AniList:** searches, show details, and (when logged in) your list and progress.
- **The streaming sites:** searches and episode requests, as a browser would.
- **AniSkip and Anime Filler List:** the show and episode you're watching, to look up
  skip times and filler episodes.
- **Discord:** what you're watching, through the Discord app on your computer
  (`discord.enabled = false` turns it off).
- **GitHub:** once a day, a check for a newer release (`general.check_updates = false`
  turns it off).

Nothing else leaves your machine. There is no telemetry.

## Contributing

Bug reports and pull requests are welcome. [docs/development.md](docs/development.md)
covers building, testing and where things live.

## License

[MIT](LICENSE)
