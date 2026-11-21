-- Migration 059: objects stored by a url.* pipeline are type "url".
--
-- Before the worker seeded the draft type, a fetched page was re-typed
-- from its body and stored as "text". Every object a url.* pipeline
-- stores was fetched from a URL, so it is the input kind "url"; the
-- subtype is the size class (short/long) or a content refinement.
-- Objects typed on purpose (repo,
-- document, image, ...) are not "text" and are left alone. Type is in
-- no FTS or vector index, so nothing needs rebuilding. Idempotent: a
-- second run matches no row.
UPDATE objects
   SET type = 'url'
 WHERE type = 'text'
   AND pipeline LIKE 'url.%';
