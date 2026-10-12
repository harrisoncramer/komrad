-- Modify "review_rounds" table
ALTER TABLE "review_rounds" VALIDATE CONSTRAINT "review_rounds_task_id_not_null";
ALTER TABLE "review_rounds" ALTER COLUMN "task_id" SET NOT NULL;
ALTER TABLE "review_rounds" DROP CONSTRAINT "review_rounds_task_id_not_null";
-- Modify "reviews" table
ALTER TABLE "reviews" VALIDATE CONSTRAINT "reviews_task_id_not_null";
ALTER TABLE "reviews" ALTER COLUMN "task_id" SET NOT NULL;
ALTER TABLE "reviews" DROP CONSTRAINT "reviews_task_id_not_null";
-- Modify "review_threads" table
ALTER TABLE "review_threads" VALIDATE CONSTRAINT "review_threads_task_id_not_null";
ALTER TABLE "review_threads" ALTER COLUMN "task_id" SET NOT NULL;
ALTER TABLE "review_threads" DROP CONSTRAINT "review_threads_task_id_not_null";
-- Modify "review_viewed_files" table
ALTER TABLE "review_viewed_files" VALIDATE CONSTRAINT "review_viewed_files_task_id_not_null";
ALTER TABLE "review_viewed_files" ALTER COLUMN "task_id" SET NOT NULL;
ALTER TABLE "review_viewed_files" DROP CONSTRAINT "review_viewed_files_task_id_not_null";
ALTER TABLE "review_viewed_files" DROP CONSTRAINT "review_viewed_files_pkey";
ALTER TABLE "review_viewed_files" ADD CONSTRAINT "review_viewed_files_pkey" PRIMARY KEY USING INDEX "review_viewed_files_task_file";
