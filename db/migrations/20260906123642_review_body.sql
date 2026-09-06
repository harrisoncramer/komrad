-- Modify "reviews" table
ALTER TABLE "reviews" ADD COLUMN "body" text NOT NULL DEFAULT '';
-- Set comment to column: "author_agent_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."author_agent_id" IS 'The agent giving the review, null when the person reviewing is. Null does not itself mean the person, author_kind says that. Deleting that agent nulls this rather than taking the review with it, since a reviewer child is deleted as a matter of course and the review it gave is what the agent it reviewed was working from.';
-- Set comment to column: "path" on table: "reviews"
COMMENT ON COLUMN "reviews"."path" IS 'The absolute path the released review was written to, under weave''s own state directory rather than in the agent''s worktree, empty while the review is still open.';
-- Set comment to column: "body" on table: "reviews"
COMMENT ON COLUMN "reviews"."body" IS 'What the author said about the work as a whole rather than about any one line, empty when they said nothing. It is written at the head of the released file, above the comments, since it is the thing the agent should read first.';
-- Set comment to index: "reviews_open_agent" on table: "reviews"
COMMENT ON INDEX "reviews_open_agent" IS 'One open review at a time for each agent reviewing, so a reviewer cannot draft two at once. Nulls stay distinct here deliberately: counting them equal would make the second reviewer child deleted on one agent fail its own delete, since the foreign key nulls this column and would collide with the first null.';
