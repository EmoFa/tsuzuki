-- A score to send with the queued change; NULL leaves the remote score alone.
ALTER TABLE sync_queue ADD COLUMN score REAL;
