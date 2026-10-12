-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "hooks" text[] NOT NULL DEFAULT '{}';
-- Set comment to column: "hooks" on table: "agents"
COMMENT ON COLUMN "agents"."hooks" IS 'The hooks the agent''s latest launch installed, empty when that launch failed or never ran.';
