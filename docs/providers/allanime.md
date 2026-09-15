# AllAnime

Verified live 2026-09-15.

## Status
- **Search and episode lists work.** Stream sources do **not**.
- `GET https://api.allanime.day/api` is behind a Cloudflare challenge; `POST` with a JSON
  body (`Referer: https://allmanga.to`) is not.
- The `episode { sourceUrls }` query returns `AA_CRYPTO_MISSING` for both plain POST and
  the persisted-query GET (hash `d405d0ed…`, Referer `youtu-chan.com`) that curd uses.
  ani-cli has dropped AllAnime entirely.
- The current web client (allmanga.to bundles) decrypts `data.tobeparsed` responses:
  AES-256-GCM, key = SHA-256(`"Xot36i3lK3:v" + version`), blob = `version(1) | iv(12) |
  ciphertext | tag(16)`. The client code contains no reference to `AA_CRYPTO`, so what
  the server expects on the request side is still unknown. Episode pages load Cloudflare
  Turnstile, and the API client retries with `extensions.captcha` on `NEED_CAPTCHA`.

## Endpoints (working)
| Purpose | GraphQL (POST JSON) |
|---|---|
| Search | `shows(search:{query,allowAdult,allowUnknown}, limit, page, translationType, countryOrigin) { edges { _id name englishName nativeName aniListId malId type thumbnail airedStart availableEpisodes } }` |
| Episodes | `show(_id) { availableEpisodesDetail }` → `{sub:[…], dub:[…], raw:[…]}` strings, newest first |

Shows carry `aniListId` / `malId` directly (as strings, sometimes null).
