-- Migration 013: lightweight reminders on knowledge objects
ALTER TABLE objects ADD COLUMN remind_at   TEXT DEFAULT NULL;
ALTER TABLE objects ADD COLUMN reminded_at TEXT DEFAULT NULL;

CREATE INDEX IF NOT EXISTS idx_objects_remind_at ON objects(remind_at)
    WHERE remind_at IS NOT NULL;
