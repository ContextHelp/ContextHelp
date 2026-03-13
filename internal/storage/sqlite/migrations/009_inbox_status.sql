ALTER TABLE objects ADD COLUMN status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE objects ADD COLUMN inbox_note TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_objects_status ON objects(status);
