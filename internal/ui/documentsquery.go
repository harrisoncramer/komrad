package ui

import (
	"context"
	"slices"
	"strings"
	"time"

	"komrad/internal/searchsyntax"
	"komrad/internal/ui/api"
	"komrad/internal/ui/panel"
	agentv1 "komrad/proto/gen/agent/v1"

	"connectrpc.com/connect"
)

func (d *documentsScreen) queryGrammar() (queryGrammar, bool) {
	agents := d.deps.clients.Agents
	grammar := queryGrammar{
		syntax: searchsyntax.Documents,
		check:  checkDocumentsClause,
		search: func(ctx context.Context, text string) (map[string][]saidHit, error) {
			return searchDocumentsSaid(ctx, agents, text)
		},
	}

	return grammar, d.kind == documentKindDocuments
}

func checkDocumentsClause(clause searchsyntax.Clause, now time.Time) error {
	switch clause.Key {
	case searchsyntax.KeySince:
		_, err := searchsyntax.ParseStart(clause.Key, clause.Value, now)

		return err
	case searchsyntax.KeyUntil:
		_, err := searchsyntax.ParseEnd(clause.Key, clause.Value, now)

		return err
	}

	return nil
}

// documentRowFits matches a document against a qualifier, by the agent that wrote it for agent:
// and workflow:, by its task for task:, and by what the server found it saying for says:.
func (m *model) documentRowFits(l panel.Listing) func(documentRow) clauseFits {
	return func(r documentRow) clauseFits {
		return func(clause searchsyntax.Clause) bool {
			item := r.item
			switch clause.Key {
			case searchsyntax.KeyAgent:
				if agentID, ok := m.openedForAgent(l, clause.Value); ok {
					return item.agentID == agentID
				}

				return containsFold(findAgentName(l, item.agentID), clause.Value)
			case searchsyntax.KeyTask:
				if taskID, ok := m.openedForTask(l, clause.Value); ok {
					return item.taskID == taskID
				}
				task, ok := l.Tasks[item.taskID]

				return ok && containsFold(task.Slug, clause.Value)
			case searchsyntax.KeyWorkflow:
				agent, ok := l.AnyAgent(item.agentID)

				return ok && containsFold(l.WorkflowName(agent.Agent.WorkflowID), clause.Value)
			case searchsyntax.KeyType:
				return strings.EqualFold(item.typeName, clause.Value)
			case searchsyntax.KeySince:
				at, err := searchsyntax.ParseStart(clause.Key, clause.Value, m.filter.qualifiers.compiledAt)

				return err == nil && !item.updatedAt.Before(at)
			case searchsyntax.KeyUntil:
				at, err := searchsyntax.ParseEnd(clause.Key, clause.Value, m.filter.qualifiers.compiledAt)

				return err == nil && item.updatedAt.Before(at)
			case searchsyntax.KeySays:
				return m.filter.hasSaid(item.id)
			}

			return false
		}
	}
}

// agentClause is the qualifier that picks out one agent's documents by its name.
func agentClause(name string) string {
	return quoteClause(searchsyntax.KeyAgent, name)
}

// taskClause is the qualifier that picks out one task's documents by its slug.
func taskClause(slug string) string {
	return quoteClause(searchsyntax.KeyTask, slug)
}

// quoteClause is a qualifier for a value, quoted when it holds a space.
func quoteClause(key, value string) string {
	if strings.ContainsAny(value, " \t") {
		return key + `:"` + value + `"`
	}

	return key + ":" + value
}

// namedAgentID is the one agent the filter's agent: qualifier names exactly, or the writer of the
// task the documents were opened for.
func (m *model) namedAgentID() (string, bool) {
	var names []string
	for _, clause := range m.filter.qualifiers.clauses {
		if clause.Key == searchsyntax.KeyAgent && !clause.Negated {
			names = append(names, clause.Value)
		}
	}
	if len(names) == 0 {
		return m.openedTaskWriter()
	}
	if len(names) != 1 {
		return "", false
	}
	if agentID, ok := m.openedForAgent(m.listing, names[0]); ok {
		return agentID, true
	}

	var ids []string
	for _, item := range slices.Concat(m.listing.Agents, m.listing.ArchivedAgents) {
		if strings.EqualFold(item.Agent.Name, names[0]) {
			ids = append(ids, item.Agent.ID)
		}
	}
	if len(ids) != 1 {
		return "", false
	}

	return ids[0], true
}

// openedForAgent is the agent the documents were opened for, when the name given is its name.
func (m *model) openedForAgent(l panel.Listing, name string) (string, bool) {
	agentID := m.documents.agentID
	if agentID == "" || !strings.EqualFold(findAgentName(l, agentID), name) {
		return "", false
	}

	return agentID, true
}

// openedTaskWriter is the writer of the task the documents were opened for, while the filter still
// names that task.
func (m *model) openedTaskWriter() (string, bool) {
	for _, clause := range m.filter.qualifiers.clauses {
		if clause.Key != searchsyntax.KeyTask || clause.Negated {
			continue
		}
		if _, ok := m.openedForTask(m.listing, clause.Value); ok && m.documents.agentID != "" {
			return m.documents.agentID, true
		}
	}

	return "", false
}

// openedForTask is the task the documents were opened for, when the slug given is its slug.
func (m *model) openedForTask(l panel.Listing, slug string) (string, bool) {
	taskID := m.documents.taskID
	task, ok := l.Tasks[taskID]
	if taskID == "" || !ok || !strings.EqualFold(task.Slug, slug) {
		return "", false
	}

	return taskID, true
}

// searchDocumentsSaid asks the server which documents hold the text as they stand now, with the passages it was found in keyed by document id.
func searchDocumentsSaid(ctx context.Context, client api.AgentReads, text string) (map[string][]saidHit, error) {
	resp, err := client.SearchDocuments(ctx, connect.NewRequest(&agentv1.SearchDocumentsRequest{
		Query: phrase(text),
		Limit: saysLimit,
	}))
	if err != nil {
		return nil, err
	}

	hits := map[string][]saidHit{}
	for _, hit := range slices.Concat(resp.Msg.GetMatches(), resp.Msg.GetNear()) {
		hits[hit.GetDocumentId()] = append(hits[hit.GetDocumentId()], saidHit{snippet: hit.GetSnippet()})
	}

	return hits, nil
}
