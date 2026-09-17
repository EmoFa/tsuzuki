-- Per-show settings that override the config. NULL columns inherit it.
CREATE TABLE show_prefs (
    media_id      INTEGER PRIMARY KEY,
    sub_languages TEXT,    -- comma-separated language codes
    sub_show      INTEGER, -- 0 or 1
    updated_at    INTEGER NOT NULL
);
