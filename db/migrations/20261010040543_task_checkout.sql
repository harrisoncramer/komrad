-- Modify "tasks" table
ALTER TABLE "tasks" ADD COLUMN "cwd" text NULL, ADD COLUMN "branch" text NULL, ADD COLUMN "adopted_branch" text NULL, ADD COLUMN "provision_enabled" boolean NULL, ADD COLUMN "worktree_removed" boolean NULL, ADD COLUMN "repository_url" text NULL, ADD COLUMN "head_sha" text NULL, ADD COLUMN "last_rebase" jsonb NULL, ADD COLUMN "sequence" "agent_sequence" NULL, ADD COLUMN "lines_added" integer NULL, ADD COLUMN "lines_removed" integer NULL, ADD COLUMN "moved_since_round" text NULL;
-- Set comment to column: "cwd" on table: "tasks"
COMMENT ON COLUMN "tasks"."cwd" IS 'The directory of the task''s checkout, which every agent on the task works in. Null while the task has none.';
-- Set comment to column: "branch" on table: "tasks"
COMMENT ON COLUMN "tasks"."branch" IS 'The git branch the task''s checkout is on, null while it has no checkout or git cannot read it.';
-- Set comment to column: "adopted_branch" on table: "tasks"
COMMENT ON COLUMN "tasks"."adopted_branch" IS 'The branch the task''s checkout was put on rather than one komrad made for it, null when komrad made it.';
-- Set comment to column: "provision_enabled" on table: "tasks"
COMMENT ON COLUMN "tasks"."provision_enabled" IS 'Whether the task''s checkout was provisioned for it: unset inherits the workflow''s setting, and false means the checkout is a directory it was given.';
-- Set comment to column: "worktree_removed" on table: "tasks"
COMMENT ON COLUMN "tasks"."worktree_removed" IS 'Whether the task''s checkout has been taken away, null while it has none.';
-- Set comment to column: "repository_url" on table: "tasks"
COMMENT ON COLUMN "tasks"."repository_url" IS 'The canonical web address of the repository the task''s checkout pushes to, such as https://github.com/harrisoncramer/komrad. Null when the checkout is gone or its origin is not a forge.';
-- Set comment to column: "head_sha" on table: "tasks"
COMMENT ON COLUMN "tasks"."head_sha" IS 'The commit the task''s checkout has checked out, null when the checkout is gone or git cannot read it.';
-- Set comment to column: "last_rebase" on table: "tasks"
COMMENT ON COLUMN "tasks"."last_rebase" IS 'What the latest rebase of the task''s checkout came to, as the result or the failure; null until its first rebase.';
-- Set comment to column: "sequence" on table: "tasks"
COMMENT ON COLUMN "tasks"."sequence" IS 'What git has part-way through the task''s checkout and waiting on somebody, null while it has no checkout.';
-- Set comment to column: "lines_added" on table: "tasks"
COMMENT ON COLUMN "tasks"."lines_added" IS 'How many lines the task''s checkout adds since it left its base, committed and uncommitted alike, null when there is no base to measure against.';
-- Set comment to column: "lines_removed" on table: "tasks"
COMMENT ON COLUMN "tasks"."lines_removed" IS 'How many lines the task''s checkout removes since it left its base, null whenever lines_added is.';
-- Set comment to column: "moved_since_round" on table: "tasks"
COMMENT ON COLUMN "tasks"."moved_since_round" IS 'The snapshot of the newest review round of the task''s agents when its checkout no longer hashes to it, null or empty while it still does or when there is no round.';
-- Create "task_pull_requests" table
CREATE TABLE "task_pull_requests" (
  "task_id" uuid NOT NULL,
  "pull_request_id" uuid NOT NULL,
  "first_seen_at" timestamptz NOT NULL DEFAULT now(),
  "last_seen_at" timestamptz NOT NULL DEFAULT now(),
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("task_id", "pull_request_id"),
  CONSTRAINT "task_pull_requests_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "task_pull_requests_pull_request_id_fkey" FOREIGN KEY ("engine_id", "pull_request_id") REFERENCES "pull_requests" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "task_pull_requests_task_id_fkey" FOREIGN KEY ("engine_id", "task_id") REFERENCES "tasks" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "task_pull_requests_pull_request_id" to table: "task_pull_requests"
CREATE INDEX "task_pull_requests_pull_request_id" ON "task_pull_requests" ("pull_request_id");
-- Set comment to table: "task_pull_requests"
COMMENT ON TABLE "task_pull_requests" IS 'Which tasks were seen with which pull requests, one row per pair, never re-pointed. The watch carries every pull request with a row here.';
-- Set comment to column: "last_seen_at" on table: "task_pull_requests"
COMMENT ON COLUMN "task_pull_requests"."last_seen_at" IS 'When the pair was last seen together, moved at most once an hour.';
-- Set comment to column: "engine_id" on table: "task_pull_requests"
COMMENT ON COLUMN "task_pull_requests"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';
