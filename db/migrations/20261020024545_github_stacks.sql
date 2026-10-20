-- Modify "pull_requests" table
ALTER TABLE "pull_requests" ADD COLUMN "stack_number" integer NULL, ADD COLUMN "stack_position" integer NULL, ADD COLUMN "stack_size" integer NULL, ADD COLUMN "stacking_refused_at" timestamptz NULL, ADD COLUMN "rebase_awaited_since" timestamptz NULL, ADD COLUMN "rebase_awaited_from" text NULL;
-- Set comment to column: "stack_number" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."stack_number" IS 'The number of the GitHub stack the pull request is in, null while it is in none.';
-- Set comment to column: "stack_position" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."stack_position" IS 'Where the pull request sits in its GitHub stack, counting from one at the bottom, null while it is in none.';
-- Set comment to column: "stack_size" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."stack_size" IS 'How many pull requests the GitHub stack holds, null while the pull request is in none.';
-- Set comment to column: "stacking_refused_at" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."stacking_refused_at" IS 'The changed_at the pull request had when GitHub last refused to put it in a GitHub stack, which komrad asks again only once changed_at moves past; null while nothing was refused.';
-- Set comment to column: "rebase_awaited_since" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."rebase_awaited_since" IS 'When komrad began waiting on GitHub to rebase the pull request''s branch after the pull request under it in its GitHub stack merged, null while komrad waits on nothing.';
-- Set comment to column: "rebase_awaited_from" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."rebase_awaited_from" IS 'The head commit the pull request had when komrad began waiting on GitHub to rebase it, which GitHub''s push moves past; null while komrad waits on nothing.';
