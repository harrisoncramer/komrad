-- Modify "review_messages" table
ALTER TABLE "review_messages" ADD COLUMN "round_id" uuid NULL, ADD CONSTRAINT "review_messages_round_id_fkey" FOREIGN KEY ("engine_id", "round_id") REFERENCES "review_rounds" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Set comment to column: "round_id" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."round_id" IS 'The round an agent''s reply said answers the thread, taken of its worktree as it stood when it replied. Set only on a reply that resolves the thread; null on everything else.';
