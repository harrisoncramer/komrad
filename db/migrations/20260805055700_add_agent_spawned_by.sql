-- Modify "agents" table
ALTER TABLE "agents" ADD COLUMN "spawned_by_agent_id" uuid NULL, ADD CONSTRAINT "agents_spawned_by_agent_id_fkey" FOREIGN KEY ("spawned_by_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
