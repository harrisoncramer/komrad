-- Modify "review_rounds" table
ALTER TABLE "review_rounds" ADD COLUMN "landed_at" timestamptz NULL;
-- Set comment to column: "landed_at" on table: "review_rounds"
COMMENT ON COLUMN "review_rounds"."landed_at" IS 'When the base branch was first seen holding this round whole, or a later round whole, null until then. A landed round''s settled comments are history rather than work still under review.';
