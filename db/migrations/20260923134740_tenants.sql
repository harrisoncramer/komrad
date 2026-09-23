-- Create "tenants" table
CREATE TABLE "tenants" (
  "id" uuid NOT NULL DEFAULT uuidv7(),
  "name" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "tenants_name_key" UNIQUE ("name")
);
INSERT INTO "tenants" ("id", "name") VALUES ('00000000-0000-7000-8000-000000000001', 'default');
-- Set comment to table: "tenants"
COMMENT ON TABLE "tenants" IS 'An owner of rows, every other table carrying the id of the tenant it belongs to.';
-- Modify "agent_metadata" table
ALTER TABLE "agent_metadata" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "agent_metadata_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "agent_metadata" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "agent_metadata"
COMMENT ON COLUMN "agent_metadata"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "agent_pull_requests" table
ALTER TABLE "agent_pull_requests" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "agent_pull_requests_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "agent_pull_requests" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "agent_pull_requests"
COMMENT ON COLUMN "agent_pull_requests"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "agent_runs" table
ALTER TABLE "agent_runs" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "agent_runs_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "agent_runs" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "agent_runs"
COMMENT ON COLUMN "agent_runs"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "agent_transcript_chunks" table
ALTER TABLE "agent_transcript_chunks" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "agent_transcript_chunks_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "agent_transcript_chunks" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "agent_transcript_chunks"
COMMENT ON COLUMN "agent_transcript_chunks"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "agent_transcript_sessions" table
ALTER TABLE "agent_transcript_sessions" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "agent_transcript_sessions_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "agent_transcript_sessions" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "agent_transcript_sessions"
COMMENT ON COLUMN "agent_transcript_sessions"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "agents_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "agents" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "agents"
COMMENT ON COLUMN "agents"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "branch_scans" table
ALTER TABLE "branch_scans" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "branch_scans_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "branch_scans" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "branch_scans"
COMMENT ON COLUMN "branch_scans"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "forge_connections" table
ALTER TABLE "forge_connections" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "forge_connections_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "forge_connections" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "forge_connections"
COMMENT ON COLUMN "forge_connections"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "mail" table
ALTER TABLE "mail" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "mail_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "mail" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "mail"
COMMENT ON COLUMN "mail"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "marks" table
ALTER TABLE "marks" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "marks_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "marks" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "marks"
COMMENT ON COLUMN "marks"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "pull_request_syncs" table
ALTER TABLE "pull_request_syncs" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "pull_request_syncs_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "pull_request_syncs" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "pull_request_syncs"
COMMENT ON COLUMN "pull_request_syncs"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "pull_requests" table
ALTER TABLE "pull_requests" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "pull_requests_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "pull_requests" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "review_messages" table
ALTER TABLE "review_messages" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "review_messages_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "review_messages" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "review_rounds" table
ALTER TABLE "review_rounds" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "review_rounds_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "review_rounds" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "review_rounds"
COMMENT ON COLUMN "review_rounds"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "review_threads" table
ALTER TABLE "review_threads" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "review_threads_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "review_threads" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "review_viewed_files" table
ALTER TABLE "review_viewed_files" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "review_viewed_files_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "review_viewed_files" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "review_viewed_files"
COMMENT ON COLUMN "review_viewed_files"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "reviews" table
ALTER TABLE "reviews" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "reviews_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "reviews" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "schedules" table
ALTER TABLE "schedules" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "schedules_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "schedules" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "schedules"
COMMENT ON COLUMN "schedules"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "workflow_configs" table
ALTER TABLE "workflow_configs" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "workflow_configs_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "workflow_configs" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "workflow_configs"
COMMENT ON COLUMN "workflow_configs"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "workflow_hooks" table
ALTER TABLE "workflow_hooks" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "workflow_hooks_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "workflow_hooks" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "workflow_hooks"
COMMENT ON COLUMN "workflow_hooks"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "workflow_provisioner_configs" table
ALTER TABLE "workflow_provisioner_configs" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "workflow_provisioner_configs_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "workflow_provisioner_configs" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "workflow_provisioner_configs"
COMMENT ON COLUMN "workflow_provisioner_configs"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "workflows" table
ALTER TABLE "workflows" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "workflows_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "workflows" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "workflows"
COMMENT ON COLUMN "workflows"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
-- Modify "worktree_sizes" table
ALTER TABLE "worktree_sizes" ADD COLUMN "tenant_id" uuid NOT NULL DEFAULT '00000000-0000-7000-8000-000000000001'::uuid, ADD CONSTRAINT "worktree_sizes_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
ALTER TABLE "worktree_sizes" ALTER COLUMN "tenant_id" SET DEFAULT (current_setting('weave.tenant'::text, true))::uuid;
-- Set comment to column: "tenant_id" on table: "worktree_sizes"
COMMENT ON COLUMN "worktree_sizes"."tenant_id" IS 'The tenant that owns this row, filled from the connection''s weave.tenant setting.';
