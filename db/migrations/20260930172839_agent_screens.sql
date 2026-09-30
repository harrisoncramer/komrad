-- Create "agent_screens" table
CREATE TABLE "agent_screens" (
  "agent_id" uuid NOT NULL,
  "frame" text NOT NULL,
  "width" integer NOT NULL,
  "height" integer NOT NULL,
  "captured_at" timestamptz NOT NULL,
  "engine_id" uuid NOT NULL,
  PRIMARY KEY ("agent_id"),
  CONSTRAINT "agent_screens_agent_id_fkey" FOREIGN KEY ("engine_id", "agent_id") REFERENCES "agents" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "agent_screens_engine_id_fkey" FOREIGN KEY ("engine_id") REFERENCES "engines" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "agent_screens_sized" CHECK ((width > 0) AND (height > 0))
);
-- Set comment to table: "agent_screens"
COMMENT ON TABLE "agent_screens" IS 'The last screen an agent''s pane drew while it was at rest, kept so the panel can show it once the pane is gone.';
-- Set comment to column: "frame" on table: "agent_screens"
COMMENT ON COLUMN "agent_screens"."frame" IS 'The pane''s visible grid as tmux captured it, colour escapes included.';
-- Set comment to column: "engine_id" on table: "agent_screens"
COMMENT ON COLUMN "agent_screens"."engine_id" IS 'The engine that owns this row, stamped from the login that wrote it.';

CREATE TRIGGER stamp_engine BEFORE INSERT ON agent_screens FOR EACH ROW EXECUTE FUNCTION stamp_engine();

ALTER TABLE agent_screens ENABLE ROW LEVEL SECURITY;

CREATE POLICY engine_isolation ON agent_screens USING (engine_id = (SELECT current_engine())) WITH CHECK (engine_id = (SELECT current_engine()));
