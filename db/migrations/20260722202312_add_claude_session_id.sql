-- Add column "claude_session_id" to table: "sessions"
ALTER TABLE `sessions` ADD COLUMN `claude_session_id` text NOT NULL DEFAULT '';
