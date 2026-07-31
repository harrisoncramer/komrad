-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "deleted_at" timestamptz NULL;
-- Backfill the rows the dropped flag had marked deleted
UPDATE "agents" SET "deleted_at" = now() WHERE "is_deleted";
-- Drop index "agents_name_active" from table: "agents"
DROP INDEX "agents_name_active";
-- Modify "agents" table
ALTER TABLE "agents" DROP COLUMN "is_deleted";
-- Create index "agents_name_active" to table: "agents"
CREATE UNIQUE INDEX "agents_name_active" ON "agents" ("name") WHERE (deleted_at IS NULL);
