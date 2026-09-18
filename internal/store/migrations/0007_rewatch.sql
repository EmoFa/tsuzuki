-- Rewatching a show starts a new round: progress counts from episode 1 again
-- while earlier rounds stay in history.
CREATE TABLE show_rounds (
    media_id   INTEGER PRIMARY KEY,
    round      INTEGER NOT NULL,
    started_at INTEGER NOT NULL
);

-- watch_progress gains the round it belongs to. SQLite can't change a primary
-- key, so the table is rebuilt; everything watched so far is round 1.
CREATE TABLE watch_progress_new (
    media_id    INTEGER NOT NULL,
    episode     REAL NOT NULL,
    round       INTEGER NOT NULL DEFAULT 1,
    position_ms INTEGER NOT NULL,
    duration_ms INTEGER NOT NULL,
    completed   INTEGER NOT NULL DEFAULT 0,
    provider    TEXT NOT NULL,
    mode        TEXT NOT NULL,
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (media_id, episode, round)
);

INSERT INTO watch_progress_new (media_id, episode, round, position_ms, duration_ms, completed, provider, mode, updated_at)
SELECT media_id, episode, 1, position_ms, duration_ms, completed, provider, mode, updated_at FROM watch_progress;

DROP TABLE watch_progress;
ALTER TABLE watch_progress_new RENAME TO watch_progress;

CREATE INDEX watch_progress_recent ON watch_progress (updated_at DESC);
