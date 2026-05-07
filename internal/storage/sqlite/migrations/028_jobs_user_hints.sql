-- T-0573: user-asserted hints on jobs (passed via `ctxt capture --hint`).
-- Stored as JSON array of hint strings (e.g. ["#ux", "research"]).
-- Worker reads this column and pre-populates draft.Tags (with Source:"user")
-- before the pipeline runs so user-asserted hints survive the auto-tagger
-- merge and become real Tag entries on the persisted KnowledgeObject.
--
-- Mirrors migration 027 (user_mentions). The auto-tagger merges with
-- existing user-source tags rather than overwriting (T-0573).
ALTER TABLE jobs ADD COLUMN user_hints TEXT DEFAULT '';
