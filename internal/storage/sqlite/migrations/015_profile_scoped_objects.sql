-- T-0088: profile-scoped objects
-- profile_id="" means the object belongs to the global (default) scope.
ALTER TABLE objects ADD COLUMN profile_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_objects_profile_id ON objects (profile_id);
