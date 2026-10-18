-- Modify "tasks" table
ALTER TABLE "tasks" ADD COLUMN "parent_task_id" uuid NULL, ADD COLUMN "fork_point" text NULL, ADD COLUMN "parent_pull_request_id" uuid NULL, ADD COLUMN "cascade_blocked" text NULL, ADD CONSTRAINT "tasks_parent_pull_request_id_fkey" FOREIGN KEY ("engine_id", "parent_pull_request_id") REFERENCES "pull_requests" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("parent_pull_request_id"), ADD CONSTRAINT "tasks_parent_task_id_fkey" FOREIGN KEY ("engine_id", "parent_task_id") REFERENCES "tasks" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("parent_task_id");
-- Create index "tasks_parent_task_id" to table: "tasks"
CREATE INDEX "tasks_parent_task_id" ON "tasks" ("parent_task_id");
-- Set comment to column: "parent_task_id" on table: "tasks"
COMMENT ON COLUMN "tasks"."parent_task_id" IS 'The task in the same workflow this task depends on, whose branch this task''s branch starts from and is measured against; null for a task on the workflow''s base branch.';
-- Set comment to column: "fork_point" on table: "tasks"
COMMENT ON COLUMN "tasks"."fork_point" IS 'The fork point, the commit of the parent''s branch this task''s own commits sit on, which rounds and line counts measure from; null while the task has no parent.';
-- Set comment to column: "parent_pull_request_id" on table: "tasks"
COMMENT ON COLUMN "tasks"."parent_pull_request_id" IS 'The parent''s pull request, read for the parent''s newest commit and its merge; null until the parent has one or while the task has no parent.';
-- Set comment to column: "cascade_blocked" on table: "tasks"
COMMENT ON COLUMN "tasks"."cascade_blocked" IS 'Why the cascade cannot rebase this task onto its parent: conflicts when the rebase stopped on conflicts, orphaned when its branch holds neither its fork point nor the parent''s newest commit, closed when the parent''s pull request closed without merging; null while nothing blocks it.';
