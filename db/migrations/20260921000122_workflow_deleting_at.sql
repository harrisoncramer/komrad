-- Modify "workflows" table
ALTER TABLE "workflows" ADD COLUMN "deleting_at" timestamptz NULL;
-- Set comment to column: "deleting_at" on table: "workflows"
COMMENT ON COLUMN "workflows"."deleting_at" IS 'When a delete claimed the workflow, which happens when a delete claims the agents inside it. A claimed workflow is left out of the archive until the last of those agents goes and takes the row with it, and an agent whose delete fails gives the workflow back as it comes back itself.';
