-- Drop index "schedules_name_active" from table: "schedules"
DROP INDEX "schedules_name_active";
-- Add the column rather than the drop and add the diff produces, which would throw
-- away which rows were put away and so bring every archived row back
ALTER TABLE "schedules" ADD COLUMN "archived_at" timestamptz NULL;
UPDATE "schedules" SET "archived_at" = now() WHERE "is_deleted";
ALTER TABLE "schedules" DROP COLUMN "is_deleted";
-- Create index "schedules_name_active" to table: "schedules"
CREATE UNIQUE INDEX "schedules_name_active" ON "schedules" ("name") WHERE (archived_at IS NULL);
-- Modify "workflows" table, the same way
ALTER TABLE "workflows" ADD COLUMN "archived_at" timestamptz NULL;
UPDATE "workflows" SET "archived_at" = now() WHERE "is_deleted";
ALTER TABLE "workflows" DROP COLUMN "is_deleted";
