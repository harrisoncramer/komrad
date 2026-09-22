-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "alive" boolean NOT NULL DEFAULT false, ADD COLUMN "tree_standing" text NOT NULL DEFAULT '', ADD COLUMN "tree_standing_sha" text NOT NULL DEFAULT '';
-- Set comment to column: "alive" on table: "agents"
COMMENT ON COLUMN "agents"."alive" IS 'Whether the agent still owns the pane its row names.';
-- Set comment to column: "tree_standing" on table: "agents"
COMMENT ON COLUMN "agents"."tree_standing" IS 'Where the worktree stands against the round its last verdict was given on, empty while it still hashes to it.';
-- Set comment to column: "tree_standing_sha" on table: "agents"
COMMENT ON COLUMN "agents"."tree_standing_sha" IS 'The round the standing was measured against.';
-- Backfill "alive" from the pane each row names.
UPDATE "agents" SET "alive" = true WHERE "pane_id" <> '';
