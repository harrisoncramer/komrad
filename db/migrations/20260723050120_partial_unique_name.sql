-- Drop index "sessions_name" from table: "sessions"
DROP INDEX `sessions_name`;
-- Create index "sessions_name_active" to table: "sessions"
CREATE UNIQUE INDEX `sessions_name_active` ON `sessions` (`name`) WHERE is_deleted = 0;
