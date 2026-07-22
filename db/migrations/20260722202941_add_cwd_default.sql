-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Create "new_sessions" table
CREATE TABLE `new_sessions` (
  `id` text NOT NULL,
  `name` text NOT NULL,
  `cwd` text NOT NULL DEFAULT '',
  `flags` text NOT NULL DEFAULT '',
  `prompt` text NOT NULL DEFAULT '',
  `pane_id` text NOT NULL DEFAULT '',
  `state` text NOT NULL DEFAULT 'new',
  `claude_session_id` text NOT NULL DEFAULT '',
  `created_at` datetime NOT NULL DEFAULT (CURRENT_TIMESTAMP),
  `updated_at` datetime NOT NULL DEFAULT (CURRENT_TIMESTAMP),
  PRIMARY KEY (`id`)
);
-- Copy rows from old table "sessions" to new temporary table "new_sessions"
INSERT INTO `new_sessions` (`id`, `name`, `cwd`, `flags`, `prompt`, `pane_id`, `state`, `claude_session_id`, `created_at`, `updated_at`) SELECT `id`, `name`, IFNULL(`cwd`, '') AS `cwd`, `flags`, `prompt`, `pane_id`, `state`, `claude_session_id`, `created_at`, `updated_at` FROM `sessions`;
-- Drop "sessions" table after copying rows
DROP TABLE `sessions`;
-- Rename temporary table "new_sessions" to "sessions"
ALTER TABLE `new_sessions` RENAME TO `sessions`;
-- Create index "sessions_name" to table: "sessions"
CREATE UNIQUE INDEX `sessions_name` ON `sessions` (`name`);
-- Enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
