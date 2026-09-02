-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "parent_agent_id" uuid NULL, ADD CONSTRAINT "agents_parent_agent_id_fkey" FOREIGN KEY ("parent_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Create index "agents_parent_agent_id" to table: "agents"
CREATE INDEX "agents_parent_agent_id" ON "agents" ("parent_agent_id");
