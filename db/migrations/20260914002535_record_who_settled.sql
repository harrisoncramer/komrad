-- Modify "review_threads" table
ALTER TABLE "review_threads" ADD COLUMN "resolved_by_kind" "review_author_kind" NULL, ADD COLUMN "resolved_by_agent_id" uuid NULL, ADD CONSTRAINT "review_threads_resolved_by_agent_id_fkey" FOREIGN KEY ("resolved_by_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Set comment to column: "resolved_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."resolved_at" IS 'When this thread was settled, null while it is still owed an answer.';
-- Set comment to column: "resolved_by_kind" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."resolved_by_kind" IS 'Whether a person or an agent settled this thread, null while nobody has. Two parties can settle one: the agent that wrote the comment, saying the answer was good enough, and the person, who may settle anything including a thread whose author is gone.';
-- Set comment to column: "resolved_by_agent_id" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."resolved_by_agent_id" IS 'The agent that settled this thread, null when a person did or when nobody has. Do not read null as meaning a person, because resolved_by_kind is what says that. Deleting that agent clears this column rather than the settling.';
