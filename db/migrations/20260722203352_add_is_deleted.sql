-- Add column "is_deleted" to table: "sessions"
ALTER TABLE `sessions` ADD COLUMN `is_deleted` integer NOT NULL DEFAULT 0;
