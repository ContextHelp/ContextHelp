-- Migration 029: stamp existing objects.pipeline rows with @v0 (T-0579, ADR-070 §2).
--
-- Per ADR-070 the `objects.pipeline` value gains a "@vN" suffix encoding the
-- pipeline version. Pre-existing rows have no suffix; the registry treats them
-- as @v0 logically, but stamping the value makes the convention explicit
-- everywhere (logs, search responses, ctxt show, downstream selector predicates
-- like `pipeline LIKE 'text.short@v1' ...`).
--
-- Idempotent: rows that already encode a version are skipped via the
-- `NOT LIKE '%@v%'` guard. Empty pipeline values are left alone.
UPDATE objects
   SET pipeline = pipeline || '@v0'
 WHERE pipeline IS NOT NULL
   AND pipeline != ''
   AND pipeline NOT LIKE '%@v%';
