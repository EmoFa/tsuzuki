# Animepahe

Verified live 2026-09-15. Curd was used as a starting hint only.

## Protection
- `animepahe.pw` (site + API) sits behind a Cloudflare **interactive** Turnstile
  challenge (`403` + `cf-mitigated: challenge`). Headless Chromium does not pass it,
  and a DevTools-controlled window fails verification even when a human ticks the box.
- What works: a plain Chromium window (no automation) on a dedicated profile. The user
  ticks the box once and closes the window. Reopening that profile headless via CDP
  yields the `cf_clearance` cookies.
- Those cookies + the **exact** browser User-Agent work from Go's `net/http` (HTTP/2).
  They do *not* work from curl (different TLS fingerprint).
- `kwik.cx` embeds need `Referer: https://animepahe.pw/` but no cookies.
- Stream CDN (`vault-NN.uwucdn.top`) returns `403` to **any HTTP/1.1 request**,
  regardless of UA/Referer. HTTP/2 with `Referer: https://kwik.cx/` works. mpv/ffmpeg
  only speak HTTP/1.1, so playback goes through tsuzuki's local stream proxy.

## Endpoints
| Purpose | Request | Notes |
|---|---|---|
| Search | `GET /api?m=search&q={query}` | JSON `data[]`: `id`, `title`, `type`, `episodes`, `year`, `poster`, `session` (show ID used everywhere else) |
| Episodes | `GET /api?m=release&id={session}&sort=episode_asc&page={n}` | 30 per page, `last_page`. Items: `episode`, `episode2`, `title`, `duration`, `filler`, `session` (episode ID) |
| External IDs | `GET /anime/{session}` | `<meta name="anilist" content="…">`, MAL only as link `//myanimelist.net/anime/{id}` |
| Stream choices | `GET /play/{showSession}/{episodeSession}` | `<button data-src="https://kwik.cx/e/…" data-fansub data-resolution data-audio="jpn|eng" data-av1>` |
| Stream URL | `GET https://kwik.cx/e/{id}` (Referer animepahe) | Dean Edwards `eval(function(p,a,c,k,e,d)…)` packed scripts; one unpacks to `const source='https://…/uwu.m3u8'` |

## Quirks
- Later seasons continue numbering from earlier ones (Frieren S2 episodes are 29–38).
  The provider renumbers from 1 and keeps the source number.
- `data-audio`: `jpn` = sub, `eng` = dub. Each kwik link is a single-quality media
  playlist (not a master), AES-128 encrypted segments with a key on the same CDN.
