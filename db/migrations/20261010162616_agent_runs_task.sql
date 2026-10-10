-- Modify "agent_runs" table
ALTER TABLE "agent_runs" ADD COLUMN "task_id" uuid NULL, ADD CONSTRAINT "agent_runs_task_id_fkey" FOREIGN KEY ("engine_id", "task_id") REFERENCES "tasks" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
-- Create index "agent_runs_task_id_started_at" to table: "agent_runs"
CREATE INDEX "agent_runs_task_id_started_at" ON "agent_runs" ("task_id", "started_at" DESC);
-- Set comment to column: "task_id" on table: "agent_runs"
COMMENT ON COLUMN "agent_runs"."task_id" IS 'The task the agent was on when the run started, null for an agent on no task.';
