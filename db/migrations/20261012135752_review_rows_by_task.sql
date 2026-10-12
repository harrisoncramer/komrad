-- Modify "review_rounds" table
ALTER TABLE "review_rounds" DROP COLUMN "agent_id", ALTER COLUMN "task_id" SET NOT NULL;
-- Modify "review_threads" table
ALTER TABLE "review_threads" DROP COLUMN "agent_id", ALTER COLUMN "task_id" SET NOT NULL;
-- Drop index "review_viewed_files_task_file" from table: "review_viewed_files"
DROP INDEX "review_viewed_files_task_file";
-- Modify "review_viewed_files" table
ALTER TABLE "review_viewed_files" DROP CONSTRAINT "review_viewed_files_pkey", DROP COLUMN "agent_id", ALTER COLUMN "task_id" SET NOT NULL, ADD PRIMARY KEY ("task_id", "file_path");
-- Modify "reviews" table
ALTER TABLE "reviews" DROP COLUMN "agent_id", ALTER COLUMN "task_id" SET NOT NULL;
