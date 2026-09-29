-- Modify "schedules" table
ALTER TABLE "schedules" ADD CONSTRAINT "schedules_orchestrator_agent_id_fkey" FOREIGN KEY ("engine_id", "orchestrator_agent_id") REFERENCES "agents" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
