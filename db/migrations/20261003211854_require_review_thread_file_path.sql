-- Modify "review_threads" table
ALTER TABLE "review_threads" ADD CONSTRAINT "review_threads_file_path_given" CHECK (file_path <> ''::text);
-- Set comment to column: "file_path" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."file_path" IS 'The file the thread is pinned to, named the way the diff names it. That is a path relative to the repository root, and where the change left the file if it moved it.';
