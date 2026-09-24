-- Create "schedule_runs" table
CREATE TABLE "schedule_runs" (
  "id" bigint NOT NULL GENERATED ALWAYS AS IDENTITY,
  "schedule_id" uuid NOT NULL,
  "scheduled_for" timestamptz NOT NULL,
  "state" text NOT NULL,
  "attempt" integer NOT NULL,
  "max_attempts" integer NOT NULL,
  "message" text NOT NULL DEFAULT '',
  "started_at" timestamptz NOT NULL DEFAULT now(),
  "finalized_at" timestamptz NULL,
  "tenant_id" uuid NOT NULL,
  PRIMARY KEY ("tenant_id", "id"),
  CONSTRAINT "schedule_runs_occurrence_key" UNIQUE ("tenant_id", "schedule_id", "scheduled_for"),
  CONSTRAINT "schedule_runs_schedule_id_fkey" FOREIGN KEY ("tenant_id", "schedule_id") REFERENCES "schedules" ("tenant_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "schedule_runs_tenant_id_fkey" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "schedule_runs_state_known" CHECK (state = ANY (ARRAY['running'::text, 'retryable'::text, 'completed'::text, 'cancelled'::text, 'discarded'::text]))
);

CREATE TRIGGER stamp_tenant BEFORE INSERT ON schedule_runs FOR EACH ROW EXECUTE FUNCTION stamp_tenant();

ALTER TABLE schedule_runs ENABLE ROW LEVEL SECURITY;

CREATE POLICY engine_isolation ON schedule_runs USING (tenant_id = (SELECT current_tenant())) WITH CHECK (tenant_id = (SELECT current_tenant()));
