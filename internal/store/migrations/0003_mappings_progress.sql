-- Which provider show corresponds to an AniList media entry.
CREATE TABLE provider_mappings (
    media_id   INTEGER NOT NULL,
    provider   TEXT NOT NULL,
    show_id    TEXT NOT NULL,
    show_title TEXT NOT NULL,
    manual     INTEGER NOT NULL DEFAULT 0, -- set by the user; never replaced automatically
    updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
    PRIMARY KEY (media_id, provider)
);

-- Playback position per episode.
CREATE TABLE watch_progress (
    media_id    INTEGER NOT NULL,
    episode     REAL NOT NULL,
    position_ms INTEGER NOT NULL,
    duration_ms INTEGER NOT NULL,
    completed   INTEGER NOT NULL DEFAULT 0,
    provider    TEXT NOT NULL,
    mode        TEXT NOT NULL,
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (media_id, episode)
);

CREATE INDEX watch_progress_recent ON watch_progress (updated_at DESC);
