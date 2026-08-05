-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "ref" text NOT NULL DEFAULT '';
-- Agents that already exist take a ref derived from their id, since the unique index below
-- cannot be created while every one of them holds the same empty string. An md5 is written
-- in symbols the Crockford alphabet carries, so what comes out reads like a minted ref.
UPDATE
    "agents"
SET
    "ref" = upper(substr(md5("id"::text), 1, 6));
-- Create index "agents_ref" to table: "agents"
CREATE UNIQUE INDEX "agents_ref" ON "agents" ("ref");
