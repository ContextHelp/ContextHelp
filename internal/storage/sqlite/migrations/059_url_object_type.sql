-- Migration 059: URL captures are type "url".
--
-- Before the worker seeded the draft type from the job, a fetched page
-- was re-typed from its body and stored as "text". Objects fetched by a
-- url.* pipeline from an http(s) source are the input kind "url"; the
-- content kind stays in subtype. Objects a pipeline typed on purpose
-- (repo, document, image, ...) are not "text" and are left alone, as is
-- the url.repo pipeline. Type is in no FTS or vector index, so nothing
-- needs rebuilding. Idempotent: a second run matches no row.
UPDATE objects
   SET type = 'url'
 WHERE type = 'text'
   AND pipeline LIKE 'url.%'
   AND pipeline NOT LIKE 'url.repo%'
   AND LOWER(source) LIKE 'http%';
