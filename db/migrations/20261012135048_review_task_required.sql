-- Check "review_rounds_task_id_not_null" on table: "review_rounds"
ALTER TABLE "review_rounds" ADD CONSTRAINT "review_rounds_task_id_not_null" CHECK (task_id IS NOT NULL) NOT VALID;
-- Check "reviews_task_id_not_null" on table: "reviews"
ALTER TABLE "reviews" ADD CONSTRAINT "reviews_task_id_not_null" CHECK (task_id IS NOT NULL) NOT VALID;
-- Check "review_threads_task_id_not_null" on table: "review_threads"
ALTER TABLE "review_threads" ADD CONSTRAINT "review_threads_task_id_not_null" CHECK (task_id IS NOT NULL) NOT VALID;
-- Check "review_viewed_files_task_id_not_null" on table: "review_viewed_files"
ALTER TABLE "review_viewed_files" ADD CONSTRAINT "review_viewed_files_task_id_not_null" CHECK (task_id IS NOT NULL) NOT VALID;
