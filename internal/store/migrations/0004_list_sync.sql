-- The user's anime list, mirrored locally. Status uses AniList's values:
-- CURRENT, PLANNING, COMPLETED, DROPPED, PAUSED, REPEATING.
CREATE TABLE list_entries (
    media_id   INTEGER PRIMARY KEY,
    status     TEXT NOT NULL,
    progress   INTEGER NOT NULL DEFAULT 0,
    score      REAL NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL
);

CREATE INDEX list_entries_status ON list_entries (status, updated_at DESC);

-- List changes not yet saved to the remote tracker. Only the latest state per
-- show matters, so a newer change replaces an older pending one.
CREATE TABLE sync_queue (
    media_id   INTEGER PRIMARY KEY,
    status     TEXT NOT NULL,
    progress   INTEGER NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    queued_at  INTEGER NOT NULL
);
