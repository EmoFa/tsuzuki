-- How far the show had been watched when a rewatch started, so earlier
-- episodes can be marked faintly even when the first watch was only recorded
-- on the user's tracker.
ALTER TABLE show_rounds ADD COLUMN watched_before INTEGER NOT NULL DEFAULT 0;
