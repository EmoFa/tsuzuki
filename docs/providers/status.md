# Provider status

| Provider | Checked | State |
|---|---|---|
| Anikoto | 2026-09-15 | Working (first in order). Streams via headless browser on megaplay embeds; disguised segments unwrapped by the proxy. See [anikoto.md](anikoto.md). |
| Animepahe | 2026-09-15 | Working (interactive Cloudflare once, stream proxy for playback). See [animepahe.md](animepahe.md). |
| KickAssAnime | 2026-09-18 | Working. JSON API for search/episodes, HLS from its player page; multi-language audio in one manifest. See [kickassanime.md](kickassanime.md). |
| Senshi | 2026-09-15 | Working on **senshi.to** (senshi.live expired). Encrypted playlists, played via the stream proxy. See [senshi.md](senshi.md). |

AllAnime was dropped on 2026-09-18: its episode sources have answered `AA_CRYPTO_MISSING` since
the provider was written, so it only delayed every fallback, and the site isn't expected to return
until late 2028. Search and episode listing still worked, so the removed implementation is in the
git history (`internal/provider/allanime`, up to v0.2.0) if it comes back.

AniNeko was dropped on 2026-09-15: the site has been broken (database errors on every page)
and directories such as everythingmoe.com have delisted it.

## Bot protection notes (2026-09-15)

- Cloudflare and DDoS-Guard both send their `Server` header on every response, so a 403 from
  them is only treated as a solvable challenge when the page is one: Cloudflare's
  `cf-mitigated: challenge` / "Just a moment" page, or DDoS-Guard's js-challenge page.
  Cloudflare's "Attention Required" block pages (e.g. for HTTP/1.1 clients) are plain errors.
- A real DDoS-Guard challenge couldn't be triggered on demand; its test fixture is built from
  markers observed on live DDoS-Guard sites (`/.well-known/ddos-guard/js-challenge/`).
- Clearances are stored per cookie domain, so subdomains share them.
- `tsuzuki debug clearances`, `debug clear-clearance <host>` and `debug reset-browser` help
  when a site's verification gets stuck.

## Provider research, 2026-09-18

Looking for a replacement for AllAnime, from [everythingmoe.com](https://everythingmoe.com)'s list.
Chose **KickAssAnime** (see above). The others, in case they're worth another look:

| Site | State when checked |
|---|---|
| Re:Anime (`reanime.to`) | Clean JSON API at `api.reanime.to/api/v1`, and search even returns AniList IDs. Streams need an account: `/api/v1/watch/…` answers 401 and every entry reports `can_watch: false`. Worth revisiting if guest playback appears. |
| AniZone (`anizone.to`) | Laravel + Livewire: the pages carry no results or links, so search, episodes and streams all need Livewire calls with a CSRF token, or a browser. Workable but brittle. |
| UniqueStream (`anime.uniquestream.net`) | A Crunchyroll mirror with a documented FastAPI (`/api/v1/openapi.json`), but it serves Widevine/FairPlay DRM, which mpv can't play. |
| anime.nexus | TanStack Start app; `/api/*` answers "Only HTML requests are supported here", so there's no usable JSON surface. |
| Miruro (`miruro.tv`) | An aggregator that scrapes other sites, behind a Cloudflare challenge. Two layers of someone else's scraping to break. |
| AnimeOnsen | `api.animeonsen.xyz` returns 403 without registered client credentials. |
