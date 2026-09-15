# Senshi

Verified live 2026-09-15 on **senshi.to** (senshi.live expired). Curd's endpoints for
senshi.live were a starting hint; request shapes, headers and decryption below come from
the live web client and its network traffic.

## Protection
- No bot challenge on the site or API. The media hosts (`s.vidcloud.se`, `*.bcdn2.se`,
  `s.anicdn.se`) sit behind a Cloudflare firewall that returns `403` unless the request
  carries `Origin: https://senshi.to` and a browser User-Agent. HTTP/1.1 is accepted.
- Playlists are **encrypted**: body is `EM3U8v1:` + base64(`iv(12) | ciphertext | tag(16)`),
  AES-256-GCM with key = XOR of two 32-byte arrays embedded in `WatchPage-*.js`. hls.js
  decrypts them in a custom loader. Segments are plain MPEG-TS (named `.jpg`).
- Because of the playlist encryption and the required headers, playback goes through the
  local stream proxy, which decrypts playlists and adds the headers.

## Endpoints
| Purpose | Request | Notes |
|---|---|---|
| Search | `POST /anime/filter` `{"searchTerm","page","limit"}` | Answers **201**. `data[]`: `id` (= MAL ID), `public_id`, `title`, `title_english`, `synonyms` (comma list), `type`, `ani_year`, `anime_picture` (relative), `sub_count`, `dub_count` |
| Show | `GET /anime/{public_id}` | Adds `anilist_id`. 404 for MAL IDs |
| Episodes | `GET /episodes/{mal_id}` | `ep_id`, `ep_title`, `ep_filler`, `ep_recap` |
| Embeds | `GET /episode-embeds/{mal_id}/{ep}` | `remote_source_id`, `status` (`Dub`, `HardSub`), `intro_start_ms`/`intro_end_ms`/`outro_start_ms`/`outro_end_ms`. `[]` for missing episodes |
| Source | `GET https://s.vidcloud.se/_v1/sources?id={remote_source_id}` | `source.src` (tokenized `master.txt`), `source.audio` (`both`), `tracks[]` (`.ass`/`.vtt` subtitles, `chapter` storyboard), `font[]` |

## Quirks
- Dub and HardSub embeds usually point at the **same** source: one master with Japanese
  and English audio renditions. Mode selects the audio track (`--alang`), not the file.
- Subtitle tracks: `sub_<lang>.ass` per language; `ai_dub.ass` labelled "English (AI Dub)"
  is what the site shows in dub mode, which suggests the English audio is an AI dub.
- Masters list 1080p and 480p variants with shared audio groups, so the master must be
  played; quality is chosen by filtering the master to one variant in the proxy.
- Catalog is small (705 shows on 2026-09-15; e.g. Frieren S1 is absent) — fallback matters.
- Per-mode availability is only a count (`sub_count`/`dub_count`); episodes above it are
  treated as unavailable in that mode.
- ASS subtitles reference custom fonts (`font[]`) that aren't loaded yet; mpv falls back
  to its default font.
- Filler/recap flags are mapped onto episodes. Intro/outro times are not used yet (Phase 8).
