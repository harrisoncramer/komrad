-- Create "session_transcripts" table
CREATE TABLE `session_transcripts` (
  `session_id` text NOT NULL,
  `content` text NOT NULL DEFAULT '',
  `synced_offset` integer NOT NULL DEFAULT 0,
  `synced_at` datetime NOT NULL DEFAULT (CURRENT_TIMESTAMP),
  PRIMARY KEY (`session_id`)
);
