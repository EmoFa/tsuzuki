# KickAssAnime

Site: <https://kaa.lt> (the domain moves; `kickassanime.mx` and `kaa.mx` were dead when this was
written). Added 2026-09-18. Implemented in `internal/provider/kickassanime`.

Everything comes from its JSON API over plain HTTPS. No browser, no challenge so far.

## Requests

| Step | Request |
|---|---|
| Search | `POST /api/fsearch` with `{"query": "frieren"}` → `result[]` with `slug`, `title`, `title_en`, `title_original`, `locales`, `year`, `type` |
| Episodes | `GET /api/show/{slug}/episodes?ep=1&lang={locale}` → `result[]` (`slug`, `episode_number`, `episode_string`, `title`, `duration_ms`) and `pages[]` for shows over 100 episodes |
| Servers | `GET /api/show/{slug}/episode/ep-{number}-{episodeSlug}` → `servers[]`, each a player URL |
| Stream | `GET` the `source=vidstream` player page → its escaped JSON state holds `https://hls.krussdomi.com/manifest/{id}/master.m3u8` and the subtitle tracks |

- **Languages.** `locales` says what a show offers: `ja-JP` is sub, `en-US` dub. Search results are
  filtered by the mode, so a sub-only show never appears in a dub search.
- **Audio.** One manifest holds every language, tagged (`jpn`, `eng`, `spa`, …), so the stream sets
  `AudioLang` and mpv picks the track. The proxy keeps only that rendition
  (`hls.KeepAudioLanguage`): a player otherwise opens all 16 audio playlists before it starts, which
  took about 50 seconds here.
- **Quality.** The master playlist offers 360p, 720p and 1080p, filtered by the proxy.
- **Headers.** The manifests, segments and subtitles all need `Referer: https://krussdomi.com/` and
  `Origin: https://krussdomi.com`, and return 403 without them, so streams go through the proxy.
- **Segments** are MPEG-TS named `.jpg`, served from rotating hosts (`st1.*.xyz`). They are not
  wrapped in a fake image header, unlike Anikoto's.
- **Subtitles.** Up to a dozen `.vtt` tracks with language codes. Their server is slow on the first
  request for a file (about 8 seconds) and fast afterwards, which is why the session warms them in
  parallel and attaches all but the preferred one after playback starts.
- **No IDs.** The API exposes no MAL or AniList ID, so `internal/mapping` matches on title, year and
  type. `type` is mapped to the shared labels ("tv" → TV).

## Notes

- The `BirdStream` server serves DASH; only `source=vidstream` (HLS) is used.
- Episode IDs are `ep-{episode_string}-{slug}`, the form the servers endpoint expects.
- Paging is capped at `maxEpisodePages` (12, i.e. about 1200 episodes).
