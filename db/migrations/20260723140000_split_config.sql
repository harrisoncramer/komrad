-- Create "session_configs" table
CREATE TABLE session_configs (
  id text NOT NULL,
  cwd text NOT NULL DEFAULT '',
  flags text NOT NULL DEFAULT '',
  created_at datetime NOT NULL DEFAULT (CURRENT_TIMESTAMP),
  updated_at datetime NOT NULL DEFAULT (CURRENT_TIMESTAMP),
  PRIMARY KEY (id)
);
-- Create "workflow_configs" table
CREATE TABLE workflow_configs (
  id text NOT NULL,
  cwd text NOT NULL DEFAULT '',
  flags text NOT NULL DEFAULT '',
  created_at datetime NOT NULL DEFAULT (CURRENT_TIMESTAMP),
  updated_at datetime NOT NULL DEFAULT (CURRENT_TIMESTAMP),
  PRIMARY KEY (id)
);
-- Backfill one config row per existing session and workflow, reusing the parent
-- id as the config id so the 1:1 link needs no generated ids.
INSERT INTO session_configs (id, cwd, flags) SELECT id, cwd, flags FROM sessions;
INSERT INTO workflow_configs (id, cwd, flags) SELECT id, '', '' FROM workflows;
-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Rebuild "sessions" without cwd/flags and with session_config_id
CREATE TABLE new_sessions (
  id text NOT NULL,
  name text NOT NULL,
  session_config_id text NOT NULL,
  prompt text NOT NULL DEFAULT '',
  pane_id text NOT NULL DEFAULT '',
  state text NOT NULL DEFAULT 'new',
  claude_session_id text NOT NULL DEFAULT '',
  workflow_id text NULL,
  is_deleted integer NOT NULL DEFAULT 0,
  created_at datetime NOT NULL DEFAULT (CURRENT_TIMESTAMP),
  updated_at datetime NOT NULL DEFAULT (CURRENT_TIMESTAMP),
  PRIMARY KEY (id)
);
INSERT INTO new_sessions (id, name, session_config_id, prompt, pane_id, state, claude_session_id, workflow_id, is_deleted, created_at, updated_at)
  SELECT id, name, id, prompt, pane_id, state, claude_session_id, workflow_id, is_deleted, created_at, updated_at FROM sessions;
DROP TABLE sessions;
ALTER TABLE new_sessions RENAME TO sessions;
CREATE UNIQUE INDEX sessions_name_active ON sessions (name) WHERE is_deleted = 0;
CREATE INDEX sessions_workflow_id ON sessions (workflow_id);
-- Rebuild "workflows" with workflow_config_id
CREATE TABLE new_workflows (
  id text NOT NULL,
  name text NOT NULL,
  workflow_config_id text NOT NULL,
  is_deleted integer NOT NULL DEFAULT 0,
  created_at datetime NOT NULL DEFAULT (CURRENT_TIMESTAMP),
  updated_at datetime NOT NULL DEFAULT (CURRENT_TIMESTAMP),
  PRIMARY KEY (id)
);
INSERT INTO new_workflows (id, name, workflow_config_id, is_deleted, created_at, updated_at)
  SELECT id, name, id, is_deleted, created_at, updated_at FROM workflows;
DROP TABLE workflows;
ALTER TABLE new_workflows RENAME TO workflows;
-- Enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
