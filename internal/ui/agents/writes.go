package agents

import (
	"fmt"
	"strings"

	"komrad/internal/ui/api"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	agentv1 "komrad/proto/gen/agent/v1"
)

// rowWrite is a write whose landing takes the panel to the row it made.
func rowWrite(clients api.Clients, doing, summary string, write func(api.Clients) screen.WriteResult) screen.Write {
	return screen.Write{
		InFlightText:   doing,
		FailureSummary: summary,
		Send:           func() screen.WriteResult { return write(clients) },
	}
}

// agentWrite is rowWrite for a write answering with an agent, whose row is shown in the
// pane the agent is running in.
func agentWrite(clients api.Clients, doing, summary string, write func(api.Clients) (*agentv1.Agent, error)) screen.Write {
	return rowWrite(clients, doing, summary, createdAgentOf(write, false))
}

// openingWrite is agentWrite for a write that puts a conversation back in a pane, which
// ends with the keyboard in that pane the way the open key over a live agent does.
func openingWrite(clients api.Clients, doing, summary string, write func(api.Clients) (*agentv1.Agent, error)) screen.Write {
	return rowWrite(clients, doing, summary, createdAgentOf(write, true))
}

// createdAgentOf turns a write answering with an agent into one landing on the agent's row.
func createdAgentOf(write func(api.Clients) (*agentv1.Agent, error), focus bool) func(api.Clients) screen.WriteResult {
	return func(clients api.Clients) screen.WriteResult {
		agent, err := write(clients)
		if err != nil {
			return screen.WriteResult{Err: err}
		}

		return createdAgent(agent, focus)
	}
}

// createdAgent lands on the row of an agent a write made, shown in the pane the agent is running in.
func createdAgent(agent *agentv1.Agent, focus bool) screen.WriteResult {
	return screen.WriteResult{
		WaitFor: screen.WaitFor{RowsShown: screen.RowsIn(screen.ListAgents, agent.GetId())},
		CreatedRow: &screen.CreatedRow{
			RowKey:          agentRowKey(agent.GetId()),
			Agent:           agent.GetId(),
			Pane:            agent.GetPaneId(),
			Harness:         agent.GetHarness(),
			ShouldFocusPane: focus,
		},
	}
}

// createdWorkflow lands on the row of a workflow, shown in the pane of the orchestrator standing
// behind it when there is one.
func createdWorkflow(workflowID string, orchestrator *agentv1.Agent) screen.WriteResult {
	return screen.WriteResult{
		WaitFor: screen.WaitFor{RowsShown: screen.RowsIn(screen.ListWorkflows, workflowID)},
		CreatedRow: &screen.CreatedRow{
			RowKey:          workflowRowKey(workflowID),
			Agent:           orchestrator.GetId(),
			Pane:            orchestrator.GetPaneId(),
			Harness:         orchestrator.GetHarness(),
			ShouldFocusPane: orchestrator != nil,
		},
	}
}

func retryWrite(clients api.Clients, agent panel.Agent, values retryFormValues) screen.Write {
	return agentWrite(clients, "retrying "+agent.Name, fmt.Sprintf("Could not retry agent %q", agent.Name), func(clients api.Clients) (*agentv1.Agent, error) {
		return clients.RetryAgent(api.RetryAgentParams{
			ID:      agent.ID,
			Flags:   values.Flags,
			Prompt:  values.Prompt,
			Harness: values.Harness,
		})
	})
}

func createWrite(clients api.Clients, params api.CreateAgentParams) screen.Write {
	return agentWrite(clients, startingLabel(params.Title), "Could not start the agent", func(clients api.Clients) (*agentv1.Agent, error) {
		return clients.CreateAgent(params)
	})
}

func startingLabel(title string) string {
	if title = strings.TrimSpace(title); title != "" {
		return "starting " + title
	}

	return "starting the agent"
}

func adoptWrite(clients api.Clients, params api.AdoptParams) screen.Write {
	return screen.Write{
		InFlightText:   "adopting " + params.Ref,
		FailureSummary: "Could not adopt",
		Send: func() screen.WriteResult {
			adopted, err := clients.Adopt(params)
			if err != nil {
				return screen.WriteResult{Err: err}
			}

			agent := adopted.GetAgent()
			landed := createdAgent(agent, false)
			if adopted.GetExisting() {
				if agent.ArchivedAt != nil {
					landed.CreatedRow.IsArchived = true
					landed.WaitFor.RowsShown = screen.RowsIn(screen.ListArchivedAgents, agent.GetId())
				}
				landed.SuccessNotice = &screen.Notice{
					Summary: "This agent already exists",
					Detail:  fmt.Sprintf("%s is already on %s", agent.GetName(), params.Ref),
				}
			}

			return landed
		},
	}
}

func createWorkflowWrite(clients api.Clients, cfg panel.WorkflowConfig) screen.Write {
	return rowWrite(clients, "creating workflow "+cfg.Name, "Could not create the workflow", func(clients api.Clients) screen.WriteResult {
		workflow, err := clients.CreateWorkflow(cfg)
		if err != nil {
			return screen.WriteResult{Err: err}
		}

		// A new workflow comes with the agent that stands behind it, and stands on its own when
		// that agent will not start.
		agent, err := clients.StartOrchestrator(workflow.GetId())
		if err != nil {
			return createdWorkflow(workflow.GetId(), nil)
		}

		return createdWorkflow(workflow.GetId(), agent)
	})
}
