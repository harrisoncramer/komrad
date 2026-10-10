-- Modify "documents" table
ALTER TABLE "documents" ADD COLUMN "task_id" uuid NULL, ADD CONSTRAINT "documents_task_id_fkey" FOREIGN KEY ("engine_id", "task_id") REFERENCES "tasks" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
-- Create index "documents_task_id" to table: "documents"
CREATE INDEX "documents_task_id" ON "documents" ("task_id");
-- Set comment to column: "task_id" on table: "documents"
COMMENT ON COLUMN "documents"."task_id" IS 'The task its author worked on when it was first written, null for an orchestrator''s document.';
-- Modify "review_rounds" table
ALTER TABLE "review_rounds" DROP CONSTRAINT "review_rounds_agent_id_fkey", ADD COLUMN "task_id" uuid NULL, ADD COLUMN "ref" text NULL, ADD CONSTRAINT "review_rounds_task_id_fkey" FOREIGN KEY ("engine_id", "task_id") REFERENCES "tasks" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
-- Create index "review_rounds_task_ordinal" to table: "review_rounds"
CREATE UNIQUE INDEX "review_rounds_task_ordinal" ON "review_rounds" ("task_id", "ordinal");
-- Set comment to column: "agent_id" on table: "review_rounds"
COMMENT ON COLUMN "review_rounds"."agent_id" IS 'The agent the round was taken through, kept for older builds while every read goes by task.';
-- Set comment to column: "ordinal" on table: "review_rounds"
COMMENT ON COLUMN "review_rounds"."ordinal" IS 'Which round this is for its task, counting from one. The panel names the round by this number.';
-- Set comment to column: "task_id" on table: "review_rounds"
COMMENT ON COLUMN "review_rounds"."task_id" IS 'The task whose checkout the round froze.';
-- Set comment to column: "ref" on table: "review_rounds"
COMMENT ON COLUMN "review_rounds"."ref" IS 'The git ref holding the snapshot alive, which is released by this name.';
-- Modify "review_threads" table
ALTER TABLE "review_threads" DROP CONSTRAINT "review_threads_agent_id_fkey", ADD COLUMN "task_id" uuid NULL, ADD CONSTRAINT "review_threads_task_id_fkey" FOREIGN KEY ("engine_id", "task_id") REFERENCES "tasks" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
-- Create index "review_threads_task_forge_thread" to table: "review_threads"
CREATE UNIQUE INDEX "review_threads_task_forge_thread" ON "review_threads" ("task_id", "forge_thread_id") WHERE (forge_thread_id IS NOT NULL);
-- Create index "review_threads_task_id" to table: "review_threads"
CREATE INDEX "review_threads_task_id" ON "review_threads" ("task_id", "file_path", "new_start", "old_start");
-- Set comment to column: "agent_id" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."agent_id" IS 'The agent its round was taken through, kept for older builds while every read goes by task.';
-- Set comment to column: "task_id" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."task_id" IS 'The task being reviewed. The review already says this and neither value ever changes. It is repeated here because almost every read of threads is by task.';
-- Set comment to index: "review_threads_task_forge_thread" on table: "review_threads"
COMMENT ON INDEX "review_threads_task_forge_thread" IS 'Holds each task to one mirror of a forge thread.';
-- Modify "review_viewed_files" table
ALTER TABLE "review_viewed_files" DROP CONSTRAINT "review_viewed_files_agent_id_fkey", ADD COLUMN "task_id" uuid NULL, ADD CONSTRAINT "review_viewed_files_task_id_fkey" FOREIGN KEY ("engine_id", "task_id") REFERENCES "tasks" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
-- Create index "review_viewed_files_task_file" to table: "review_viewed_files"
CREATE UNIQUE INDEX "review_viewed_files_task_file" ON "review_viewed_files" ("task_id", "file_path");
-- Set comment to column: "agent_id" on table: "review_viewed_files"
COMMENT ON COLUMN "review_viewed_files"."agent_id" IS 'The agent whose diff the file was last marked read in.';
-- Set comment to column: "task_id" on table: "review_viewed_files"
COMMENT ON COLUMN "review_viewed_files"."task_id" IS 'The task whose diff the file is in.';
-- Modify "reviews" table
ALTER TABLE "reviews" DROP CONSTRAINT "reviews_agent_id_fkey", ADD COLUMN "task_id" uuid NULL, ADD CONSTRAINT "reviews_task_id_fkey" FOREIGN KEY ("engine_id", "task_id") REFERENCES "tasks" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
-- Create index "reviews_open_agent_task" to table: "reviews"
CREATE UNIQUE INDEX "reviews_open_agent_task" ON "reviews" ("task_id", "author_agent_id") WHERE ((released_at IS NULL) AND (author_kind = 'agent'::review_author_kind));
-- Create index "reviews_open_human_task" to table: "reviews"
CREATE UNIQUE INDEX "reviews_open_human_task" ON "reviews" ("task_id") WHERE ((released_at IS NULL) AND (author_kind = 'human'::review_author_kind));
-- Create index "reviews_task_id" to table: "reviews"
CREATE INDEX "reviews_task_id" ON "reviews" ("task_id", "id");
-- Set comment to column: "agent_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."agent_id" IS 'The agent its round was taken through, kept for older builds while every read goes by task.';
-- Set comment to column: "task_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."task_id" IS 'The task being reviewed. The round already says this, and it is repeated here so that one open review per reviewer can be a plain unique index with no join.';
-- Set comment to index: "reviews_open_agent_task" on table: "reviews"
COMMENT ON INDEX "reviews_open_agent_task" IS 'Holds each reviewing agent to one unreleased review of a task at a time, relying on Postgres counting two null authors as different values.';
-- Set comment to index: "reviews_open_human_task" on table: "reviews"
COMMENT ON INDEX "reviews_open_human_task" IS 'Holds the person to one unreleased review of a task at a time, the way GitHub holds one pending review per reviewer.';
