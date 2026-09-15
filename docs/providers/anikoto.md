# Anikoto

Verified live 2026-09-15 on **anikototv.to**. First in the default provider order.

## Protection
- Site and AJAX endpoints sit behind Cloudflare but served no challenge when checked.
  The `vrf` query parameter the site sends is accepted empty; no cookies are needed.
- Streams come from **megaplay.buzz** embeds. `stream/getSources` returns the HLS URL
  encrypted (`enc`), decrypted only by megaplay's obfuscated player script (key material
  appears tied to a `stream/trustWatch` exchange). Rather than reimplementing that, tsuzuki
  loads the embed in a shared headless Chromium (`browser.Sniffer`) and records the
  `getSources` response and the master `.m3u8` request. This takes about 1–2s including
  browser start-up.
- The master and variant playlists need `Referer: https://megaplay.buzz/`; no IP or
  HTTP-version restrictions.
- **Segments are disguised**: each MPEG-TS segment is hosted on an image CDN
  (tiktokcdn) behind a fake 70-byte PNG, with the TS packets starting a little later
  (offset 252 when checked). ffmpeg sees a PNG and fails, so playback goes through the
  stream proxy, which strips everything before the first aligned TS packet.

## Endpoints
| Purpose | Request | Notes |
|---|---|---|
| Search | `GET /filter?keyword={q}` | `#list-items .item`: `data-tip` (anime ID), `a.name` (title, `data-jp`), `ep-status sub/dub` counts, type. `/ajax/anime/search` caps at 5 results |
| Episodes | `GET /ajax/episode/list/{id}?vrf=` (`X-Requested-With`) | JSON `result` HTML: `a[data-num][data-mal][data-sub][data-dub][data-ids]`, class `filler`, `span.d-title` |
| Servers | `GET /ajax/server/list?servers={data-ids}` | Groups `data-type="sub|dub"` with `li[data-link-id]` (HD-1, HD-2, Vidstream-2) |
| Embed | `GET /ajax/server?get={link-id}` | `result.url` (megaplay embed), `result.skip_data` intro/outro seconds |
| Sources | megaplay `GET /stream/getSources?id=…&s=…` (in browser) | `tracks[]` (vtt subtitles, several languages incl. "(AI)" ones), `intro`/`outro`, encrypted `enc` |

## Quirks
- MAL IDs are on every episode (`data-mal`), used for mapping. No AniList IDs.
- Subtitles are ordered English → other languages → machine-translated "(AI)" tracks, since
  players select the first.
- Intro/outro times are available (embed `skip_data` and getSources) but not used yet (Phase 8).
