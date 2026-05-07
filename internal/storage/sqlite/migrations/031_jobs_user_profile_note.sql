-- T-0588: user-asserted profile + note on jobs (passed via
-- `ctxt capture --profile <name>` and `ctxt capture --note <text>`).
--
-- user_profile is the focus profile slug the operator pinned at capture
-- time; the worker pre-populates draft.ProfileID before the pipeline
-- runs so the persisted KnowledgeObject is correctly partitioned.
-- user_note is a free-form audit string the operator attached to the
-- capture; the worker pre-populates draft.InboxNote (currently the
-- canonical "operator-attached" note field on KnowledgeObject — survives
-- both inbox and pipeline paths).
--
-- Mirrors migrations 027 (user_mentions, T-0190) and 028 (user_hints,
-- T-0573). Plain SQL — neither column existed in any prior schema
-- version.
ALTER TABLE jobs ADD COLUMN user_profile TEXT DEFAULT '';
ALTER TABLE jobs ADD COLUMN user_note TEXT DEFAULT '';
