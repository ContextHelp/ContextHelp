-- Migration 012: rename mentions → mention_uris
-- Alpha software — clean break, no data migration needed.
ALTER TABLE objects RENAME COLUMN mentions TO mention_uris;
