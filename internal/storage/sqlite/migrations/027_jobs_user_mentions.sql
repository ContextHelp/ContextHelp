-- T-0190: user-asserted mentions on jobs (passed via `ctxt analyze --mentions`).
-- Stored as JSON array of @-prefixed slug strings (e.g. ["@client.acme"]).
-- Worker reads this column and pre-populates draft.Mentions before the
-- pipeline runs entity extraction, so user-asserted mentions become real
-- mention edges + entity rows alongside auto-extracted ones.
ALTER TABLE jobs ADD COLUMN user_mentions TEXT DEFAULT '';
