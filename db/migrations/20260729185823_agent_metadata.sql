-- Create "agent_metadata" table
CREATE TABLE "agent_metadata" (
  "agent_id" uuid NOT NULL,
  "key" text NOT NULL,
  "value" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("agent_id", "key"),
  CONSTRAINT "agent_metadata_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Set comment to table: "agent_metadata"
COMMENT ON TABLE "agent_metadata" IS 'Key/value facts an agent recorded about its own work.';
