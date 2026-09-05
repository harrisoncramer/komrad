-- Drop index "reviews_open_agent" from table: "reviews"
DROP INDEX "reviews_open_agent";
-- Create index "reviews_open_agent" to table: "reviews"
CREATE UNIQUE INDEX "reviews_open_agent" ON "reviews" ("agent_id", "author_agent_id") NULLS NOT DISTINCT WHERE ((released_at IS NULL) AND (author_kind = 'agent'::review_author_kind));
-- Set comment to column: "author_agent_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."author_agent_id" IS 'The agent giving the review, null when the person reviewing is. Null does not itself mean the person, author_kind says that. Deleting that agent nulls this rather than taking the review with it, since a reviewer child is deleted as a matter of course and the review it gave is what the agent it reviewed was working from.';
-- Set comment to column: "path" on table: "reviews"
COMMENT ON COLUMN "reviews"."path" IS 'The absolute path the released review was written to, under weave''s own state directory rather than in the agent''s worktree, empty while the review is still open.';
-- Set comment to index: "reviews_open_agent" on table: "reviews"
COMMENT ON INDEX "reviews_open_agent" IS 'One open review at a time for each agent reviewing, so a reviewer cannot draft two at once. Nulls count as equal here so that a batch left open by a deleted reviewer stays inside the index rather than falling outside every constraint.';
