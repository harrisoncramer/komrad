-- Modify "review_threads" table
ALTER TABLE "review_threads" ADD COLUMN "round_id" uuid NULL, ADD COLUMN "harvested" boolean NOT NULL DEFAULT false;
-- Backfill "round_id" from the batch each thread was opened in, which is where the round was read from before this column existed.
UPDATE "review_threads" SET "round_id" = "reviews"."round_id" FROM "reviews" WHERE "reviews"."id" = "review_threads"."review_id" AND "review_threads"."round_id" IS NULL;
ALTER TABLE "review_threads" ALTER COLUMN "round_id" SET NOT NULL, ADD CONSTRAINT "review_threads_round_id_fkey" FOREIGN KEY ("round_id") REFERENCES "review_rounds" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Set comment to column: "round_id" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."round_id" IS 'The round the thread''s line numbers were measured in, held here rather than read through the review because a batch stays open across rounds and would report the wrong one.';
-- Set comment to column: "harvested" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."harvested" IS 'Whether weave wrote this thread from an edit of the round''s checkout rather than a person or an agent typing it, which is what makes it weave''s to rewrite or delete on a later harvest.';
