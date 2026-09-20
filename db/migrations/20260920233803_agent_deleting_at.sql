-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "deleting_at" timestamptz NULL;
-- Set comment to column: "deleting_at" on table: "agents"
COMMENT ON COLUMN "agents"."deleting_at" IS 'When a delete claimed the row. A claimed row is left out of every listing until the delete either removes it or fails, and a failure clears the claim as it records itself. The server clears every claim when it starts, since nothing survives a restart to finish one.';
