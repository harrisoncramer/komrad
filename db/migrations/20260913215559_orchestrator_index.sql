-- Create index "agents_workflow_orchestrator" to table: "agents"
CREATE UNIQUE INDEX "agents_workflow_orchestrator" ON "agents" ("workflow_id") WHERE (role = 'orchestrator'::agent_role);
