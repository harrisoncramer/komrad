-- Modify "agents" table
ALTER TABLE "agents" DROP COLUMN "mark";
-- Create "marks" table
CREATE TABLE "marks" (
  "mark" text NOT NULL,
  "agent_id" uuid NULL,
  "workflow_id" uuid NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("mark"),
  CONSTRAINT "marks_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "marks_workflow_id_fkey" FOREIGN KEY ("workflow_id") REFERENCES "workflows" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "marks_one_target" CHECK ((agent_id IS NOT NULL) <> (workflow_id IS NOT NULL))
);
-- Create index "marks_agent_id" to table: "marks"
CREATE UNIQUE INDEX "marks_agent_id" ON "marks" ("agent_id");
-- Create index "marks_workflow_id" to table: "marks"
CREATE UNIQUE INDEX "marks_workflow_id" ON "marks" ("workflow_id");
-- Set comment to table: "marks"
COMMENT ON TABLE "marks" IS 'A letter a person addresses one row by. Held by an agent or a workflow and by nothing else, so a letter names one place.';
