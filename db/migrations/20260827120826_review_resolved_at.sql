-- Modify "review_threads" table
ALTER TABLE "review_threads" ADD COLUMN "resolved_at" timestamptz NULL;
-- Set comment to column: "resolved_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."resolved_at" IS 'When the person reviewing settled this thread, null while it is still owed an answer. It is what decides whether a thread is still asking for something, which is a different question from whether the code it was written against has moved since.';
