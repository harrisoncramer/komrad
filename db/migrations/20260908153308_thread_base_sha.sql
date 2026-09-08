-- Modify "review_threads" table
ALTER TABLE "review_threads" ADD COLUMN "base_sha" text NOT NULL DEFAULT '';
-- Set comment to column: "base_sha" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."base_sha" IS 'The commit the old side of the thread''s line numbers was read against, which is the round''s own base for a thread written on the whole change and an earlier round''s snapshot for one written while the diff was narrowed.';
-- Backfill "base_sha" from the round each thread was measured in, which is what every thread written before this column existed was read against.
UPDATE "review_threads" SET "base_sha" = "review_rounds"."base_sha" FROM "review_rounds" WHERE "review_rounds"."id" = "review_threads"."round_id" AND "review_threads"."base_sha" = '';
