-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "setup_action" text NULL, ADD COLUMN "cleanup_action" text NULL;
