-- Remove watch history invented from a tracker's progress. A short-lived
-- version filled watch_progress from the user's AniList list, which put every
-- show on their list into "continue watching". Marks from a list are shown
-- from the list itself now, so these rows are noise: they have no position,
-- no duration and no provider, which playing always records.
DELETE FROM watch_progress
WHERE completed = 1 AND position_ms = 0 AND duration_ms = 0 AND provider = '' AND mode = '';
