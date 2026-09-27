-- Set comment to column: "file_path" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."file_path" IS 'The file the thread is pinned to, named the way the diff names it. That is a path relative to the repository root, and where the change left the file if it moved it. It is empty for a comment on the change as a whole, which has no file and no lines.';
