-- Create "sessions" table
CREATE TABLE `sessions` (
  `id` text NOT NULL,
  `name` text NOT NULL,
  `cwd` text NOT NULL,
  `flags` text NOT NULL DEFAULT '',
  `prompt` text NOT NULL DEFAULT '',
  `pane_id` text NOT NULL DEFAULT '',
  `state` text NOT NULL DEFAULT 'new',
  `created_at` datetime NOT NULL DEFAULT (CURRENT_TIMESTAMP),
  `updated_at` datetime NOT NULL DEFAULT (CURRENT_TIMESTAMP),
  PRIMARY KEY (`id`)
);
-- Create index "sessions_name" to table: "sessions"
CREATE UNIQUE INDEX `sessions_name` ON `sessions` (`name`);
