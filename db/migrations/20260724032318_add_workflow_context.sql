-- Add column "context" to table: "workflows"
ALTER TABLE `workflows` ADD COLUMN `context` text NOT NULL DEFAULT '';
