# Configuration

tsuzuki reads a TOML file. Every key is optional: anything you leave out uses the
default shown below, so a config file only needs the settings you want to change.

```sh
tsuzuki config init    # write a commented config with every default
tsuzuki config edit    # open it in $VISUAL / $EDITOR
tsuzuki config check   # validate it
tsuzuki config path    # show where the config, database and log live
```

Changes take effect the next time tsuzuki starts. An unknown key or invalid value
is an error that names the key, so typos don't go unnoticed.

## File locations

| | Linux | macOS | Windows |
|---|---|---|---|
| Config (`config.toml`, AniList login) | `~/.config/tsuzuki` | `~/Library/Application Support/tsuzuki` | `%AppData%\tsuzuki` |
| Data (`tsuzuki.db`: history, list, mappings) | `~/.local/share/tsuzuki` | `~/Library/Application Support/tsuzuki` | `%LocalAppData%\tsuzuki` |
| Cache (`tsuzuki.log`, downloaded Chromium) | `~/.cache/tsuzuki` | `~/Library/Caches/tsuzuki` | `%LocalAppData%\tsuzuki` |

On Linux, `XDG_CONFIG_HOME`, `XDG_DATA_HOME` and `XDG_CACHE_HOME` are respected.
Each directory can also be moved with `TSUZUKI_CONFIG_DIR`, `TSUZUKI_DATA_DIR` or
`TSUZUKI_CACHE_DIR`, and `--config <file>` reads a different config file.

---

## `[general]`

| Key | Default | Description |
|---|---|---|
| `general.mode` | `"sub"` | Which version to look for: `"sub"` (original audio with subtitles) or `"dub"`. `watch --mode` and `continue --mode` override it; `continue` otherwise keeps the mode you used last for that show. |
| `general.quality` | `"best"` | Preferred quality: `"best"`, `"1080"`, `"720"`, `"480"`, `"360"` or `"worst"`. When the exact height isn't offered, the nearest lower one is used. |
| `general.autoplay_next` | `true` | Start the next episode when one plays to the end. Stopping mpv yourself never autoplays. |
| `general.watched_threshold` | `0.85` | Fraction of an episode (greater than 0, at most 1) after which it counts as watched: saved as completed in your history, and sent to your list. |
| `general.resume_rewind_seconds` | `5` | When resuming, start this many seconds before where you stopped. |
| `general.check_updates` | `true` | Check GitHub at most once a day for a newer release. When there is one, the UI shows a notice once, with the command to upgrade for how tsuzuki was installed; `tsuzuki version` and `tsuzuki doctor` mention it too. Nothing is downloaded or installed automatically. |

## `[providers]`

| Key | Default | Description |
|---|---|---|
| `providers.order` | `["anikoto", "senshi", "animepahe", "kickassanime"]` | Providers to try, in order. Each is tried until one has the episode and its stream passes a health check; a stream that fails while playing moves on to the next provider, resuming at the same position. Remove a provider to never use it. Available: `anikoto`, `senshi`, `animepahe`, `kickassanime`. A provider tsuzuki has dropped (currently `allanime`) stays valid here so old configs keep working: it's skipped, and `doctor` says so. |
| `providers.health_check_timeout` | `"5s"` | How long to wait when checking that a stream actually serves video before handing it to mpv. A duration such as `"5s"` or `"1m"`. |

The provider that last worked for a show is tried first next time.

## `[player]`

| Key | Default | Description |
|---|---|---|
| `player.mpv_path` | `""` | Path to mpv, or the folder containing it. Empty searches the `PATH` and the usual install locations: on Windows, the folder `tsuzuki.exe` is in, mpv registered with `mpv --register`, and the Program Files, winget, Scoop and Chocolatey folders. On Windows, write the path in single quotes so backslashes are kept: `mpv_path = 'C:\Tools\mpv\mpv.exe'`. |
| `player.extra_args` | `[]` | Extra mpv arguments, e.g. `["--fullscreen", "--volume=70"]`. They come after tsuzuki's own arguments, so they win: avoid `--start` (it overrides resuming), `--input-ipc-server`, `--idle` and `--keep-open`, which tsuzuki relies on. mpv's own `mpv.conf` is also read unless you pass `--no-config`, except that tsuzuki always turns off `keep-open` and `idle` so mpv closes when an episode ends. |

## `[browser]`

tsuzuki uses Chrome or Chromium for two things: running the embedded players some
providers need (Anikoto), and getting past Cloudflare or DDoS-Guard bot checks.

| Key | Default | Description |
|---|---|---|
| `browser.path` | `""` | Path to Chrome/Chromium. Empty auto-detects an installed Chrome, Chromium or Edge. |
| `browser.headless` | `true` | Try bot checks invisibly first. When that doesn't pass (or with `false`), a normal browser window opens for you to complete the check; tsuzuki never clicks it for you. |
| `browser.auto_download` | `false` | Download a Chromium build into the cache directory when none is installed. |

## `[tracking]`

| Key | Default | Description |
|---|---|---|
| `tracking.backend` | `"anilist"` | `"anilist"`: while you're logged in (`tsuzuki login`), your list mirrors AniList. Watched episodes and status changes are sent straight away, and queued to retry when AniList can't be reached. Your list's progress is also filled in as watched episodes here (a Completed show counts as fully watched), both when syncing and when you open a show. Before you log in, progress is only kept locally. `"local"`: nothing is ever sent to AniList. |
| `tracking.anilist_client_id` | `0` | AniList API client used by `tsuzuki login`. `0` uses tsuzuki's own client. To use your own, create one at <https://anilist.co/settings/developer> with the redirect URL `http://localhost:47281/callback`. |

## `[skip]`

Opening, ending and recap times come from the provider when it has them, and
otherwise from [AniSkip](https://aniskip.com). Filler information comes from
[Anime Filler List](https://www.animefillerlist.com).

| Key | Default | Description |
|---|---|---|
| `skip.opening` | `"auto"` | At an opening: `"auto"` jumps past it, `"prompt"` shows a message and skips when you press <kbd>Tab</kbd> in mpv, `"off"` does nothing. Each opening is skipped or prompted once, so seeking back into it lets you watch it. |
| `skip.ending` | `"auto"` | The same for endings. |
| `skip.recap` | `"prompt"` | The same for recap segments within an episode. |
| `skip.filler_episodes` | `false` | Pass over filler episodes when tsuzuki chooses the episode (`continue` and autoplay). An episode you pick yourself always plays. Mixed canon/filler episodes are never skipped. |
| `skip.recap_episodes` | `false` | Pass over whole recap episodes the same way. |

## `[subtitles]`

| Key | Default | Description |
|---|---|---|
| `subtitles.languages` | `["en"]` | Preferred subtitle languages, most preferred first, as two- or three-letter codes (`"en"`, `"es"`, `"pt"`, `"de"`, …). The first track in one of them is selected; if none is offered, the first available track is. Machine-translated tracks come last. |
| `subtitles.show` | `true` | Show subtitles when an episode starts. `false` still loads them, hidden until you press <kbd>v</kbd> in mpv. |

A show can have its own subtitle settings: press <kbd>s</kbd> on its details screen. For a single
watch, `tsuzuki watch <id> --subs es,en` or `--subs off` overrides both. Animepahe's streams have
English subtitles burned into the video, so these settings can't change them.

## `[discord]`

| Key | Default | Description |
|---|---|---|
| `discord.enabled` | `true` | Show what you're watching as Discord Rich Presence: title, episode, progress and a link to AniList. Nothing happens when Discord isn't running; presence appears when it starts. |
| `discord.client_id` | `""` | Discord application ID. Empty uses tsuzuki's application. With your own (from <https://discord.com/developers/applications>), Discord shows your application's name instead of "tsuzuki". |
| `discord.show_cover` | `true` | Include the anime's cover art. Covers of adult titles are never shown. |

## `[ui]`

| Key | Default | Description |
|---|---|---|
| `ui.theme` | `"default"` | `"default"` uses your terminal's 16-colour palette, so it follows your terminal theme. `"mono"` uses no colour, only bold and dim text. The `NO_COLOR` environment variable is also respected. |

---

## Example

```toml
[general]
mode = "dub"
quality = "720"

[providers]
order = ["senshi", "anikoto"]

[player]
extra_args = ["--fullscreen"]

[skip]
recap = "auto"
filler_episodes = true

[discord]
show_cover = false
```
