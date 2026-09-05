-- Modify "reviews" table
ALTER TABLE "reviews" DROP CONSTRAINT "reviews_author_agent_id_fkey", ADD CONSTRAINT "reviews_author_agent_id_fkey" FOREIGN KEY ("author_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Set comment to column: "author_agent_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."author_agent_id" IS 'The agent giving the review, null when the person reviewing is. Null does not itself mean the person, author_kind says that. Deleting that agent takes its reviews with it rather than nulling this, since a null here would put an open batch outside the index that holds an author to one at a time and beyond any call that could find it again.';
