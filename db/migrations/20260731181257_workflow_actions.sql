-- Modify "workflows" table
ALTER TABLE "workflows" ADD COLUMN "setup_action" text NOT NULL DEFAULT '', ADD COLUMN "cleanup_action" text NOT NULL DEFAULT '';
