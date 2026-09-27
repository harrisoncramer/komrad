-- Modify "agents" table
ALTER TABLE "agents" DROP CONSTRAINT "agents_schedule_id_fkey", ADD CONSTRAINT "agents_schedule_id_fkey" FOREIGN KEY ("engine_id", "schedule_id") REFERENCES "schedules" ("engine_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("schedule_id");
