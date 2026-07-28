-- Modify "schedules" table
ALTER TABLE "schedules" ADD COLUMN "hide_agents" boolean NOT NULL DEFAULT false;
