-- Migration 024: rename mention_uris back to mentions
-- The column was renamed to mention_uris in a branch that was later reverted.
-- Code uses 'mentions'; this aligns the DB schema.
ALTER TABLE objects RENAME COLUMN mention_uris TO mentions;
