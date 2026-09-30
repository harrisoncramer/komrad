-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "lines_added" integer NULL, ADD COLUMN "lines_removed" integer NULL;
-- Set comment to column: "lines_added" on table: "agents"
COMMENT ON COLUMN "agents"."lines_added" IS 'How many lines the agent''s checkout adds since it left its base, committed and uncommitted alike, null when there is no base to measure against. The cwd syncer rewrites it every pass while the agent is live and leaves it as it was once the agent is archived.';
-- Set comment to column: "lines_removed" on table: "agents"
COMMENT ON COLUMN "agents"."lines_removed" IS 'How many lines the agent''s checkout removes since it left its base, null whenever lines_added is.';
