-- Modify "tasks" table
ALTER TABLE "tasks" ADD COLUMN "stands_on_task_id" uuid NULL, ADD COLUMN "stand_tip" text NULL, ADD COLUMN "stand_pull_request_id" uuid NULL, ADD COLUMN "restack_blocked" text NULL, ADD CONSTRAINT "tasks_stand_pull_request_id_fkey" FOREIGN KEY ("engine_id", "stand_pull_request_id") REFERENCES "pull_requests" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("stand_pull_request_id") NOT VALID, ADD CONSTRAINT "tasks_stands_on_task_id_fkey" FOREIGN KEY ("engine_id", "stands_on_task_id") REFERENCES "tasks" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("stands_on_task_id") NOT VALID;
-- Create index "tasks_stands_on_task_id" to table: "tasks"
CREATE INDEX "tasks_stands_on_task_id" ON "tasks" ("stands_on_task_id");
-- Set comment to column: "stands_on_task_id" on table: "tasks"
COMMENT ON COLUMN "tasks"."stands_on_task_id" IS 'The task in the same workflow whose branch this task''s branch was cut from and is measured against, null for a task on the workflow''s base branch.';
-- Set comment to column: "stand_tip" on table: "tasks"
COMMENT ON COLUMN "tasks"."stand_tip" IS 'The commit of the parent''s branch this task''s own commits sit on, which rounds and line counts measure from; null while the task stands on nothing.';
-- Set comment to column: "stand_pull_request_id" on table: "tasks"
COMMENT ON COLUMN "tasks"."stand_pull_request_id" IS 'The parent''s pull request this task follows for the parent''s tip and merge, null until the parent has one or while the task stands on nothing.';
-- Set comment to column: "restack_blocked" on table: "tasks"
COMMENT ON COLUMN "tasks"."restack_blocked" IS 'Why komrad cannot restack a standing task onto its parent: conflicts when the replay conflicted, lost when its branch holds neither the recorded tip nor the parent''s; null while nothing blocks it.';
