package agents

import (
	"fmt"

	"komrad/internal/ui/list"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"
)

// refsOf is the tree as the selection sees it.
func refsOf(rows []agentRow) []list.Ref {
	refs := make([]list.Ref, 0, len(rows))
	var task string
	for _, row := range rows {
		switch {
		case row.isTask():
			task = rowKey(row)
		case !row.underTask:
			task = ""
		}
		ref := list.Ref{
			Key:      toTreeRowKey(rowKey(row)),
			IsHeader: row.header,
			Group:    workflowRowKey(row.workflow.ID),
			TaskKey:  task,
		}
		if writer, ok := row.writer(); ok {
			ref.WriterKey = agentRowKey(writer.Agent.ID)
		}
		refs = append(refs, ref)
	}

	return refs
}

func chosenAgents(sel list.Selection, rows []agentRow) []panel.Agent {
	var agents []panel.Agent
	for _, row := range rows {
		writer, ok := row.writer()
		switch {
		case !row.header && sel.Has(toTreeRowKey(rowKey(row))):
			agents = append(agents, row.item.Agent)
		case ok && sel.Has(agentRowKey(writer.Agent.ID)):
			agents = append(agents, writer.Agent)
		}
	}

	return xslice.UniqueBy(agents, func(agent panel.Agent) string { return agent.ID })
}

func describeAgents(agents []panel.Agent) string {
	if len(agents) == 1 {
		return fmt.Sprintf("agent %q", agents[0].Name)
	}

	return fmt.Sprintf("%d agents", len(agents))
}
