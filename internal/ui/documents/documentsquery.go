package documents

import (
	"context"
	"slices"
	"strings"
	"time"

	"komrad/internal/searchsyntax"
	"komrad/internal/ui/api"
	"komrad/internal/ui/list"
	"komrad/internal/ui/screen"
	"komrad/lib/xstr"
	agentv1 "komrad/proto/gen/agent/v1"

	"connectrpc.com/connect"
)

// QueryGrammar is the qualifiers the filter reads over the documents, and false on the types.
func (d *Screen) QueryGrammar() (list.Grammar, bool) {
	agents := d.deps.Clients.Agents
	grammar := list.Grammar{
		Syntax: searchsyntax.Documents,
		Check:  checkClause,
		Search: func(ctx context.Context, said string) (map[string][]list.SaidHit, error) {
			return searchSaid(ctx, agents, said)
		},
	}

	return grammar, d.kind == kindDocuments
}

func checkClause(clause searchsyntax.Clause, now time.Time) error {
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

// rowFits matches a document against a qualifier, by the agent that wrote it for agent:
// and workflow:, by its task for task:, and by what the server found it saying for says:.
func (d *Screen) rowFits(in screen.Inputs) func(row) list.Fits {
	return func(r row) list.Fits {
		return func(clause searchsyntax.Clause) bool {
			item := r.item
			switch clause.Key {
			case searchsyntax.KeyAgent:
				if agentID, ok := d.openedForAgent(in, clause.Value); ok {
					return item.agentID == agentID
				}

				return xstr.ContainsFold(findAgentName(in.Listing, item.agentID), clause.Value)
			case searchsyntax.KeyTask:
				if taskID, ok := d.openedForTask(in, clause.Value); ok {
					return item.taskID == taskID
				}
				task, ok := in.Listing.Tasks[item.taskID]

				return ok && xstr.ContainsFold(task.Slug, clause.Value)
			case searchsyntax.KeyWorkflow:
				agent, ok := in.Listing.AnyAgent(item.agentID)

				return ok && xstr.ContainsFold(in.Listing.WorkflowName(agent.Agent.WorkflowID), clause.Value)
			case searchsyntax.KeyType:
				return strings.EqualFold(item.typeName, clause.Value)
			case searchsyntax.KeySince:
				at, err := searchsyntax.ParseStart(clause.Key, clause.Value, in.Room.Pattern.CompiledAt)

				return err == nil && !item.updatedAt.Before(at)
			case searchsyntax.KeyUntil:
				at, err := searchsyntax.ParseEnd(clause.Key, clause.Value, in.Room.Pattern.CompiledAt)

				return err == nil && item.updatedAt.Before(at)
			case searchsyntax.KeySays:
				return in.Room.Pattern.HasSaid(item.id)
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
func (d *Screen) namedAgentID(in screen.Inputs) (string, bool) {
	var names []string
	for _, clause := range in.Room.Pattern.Clauses {
		if clause.Key == searchsyntax.KeyAgent && !clause.Negated {
			names = append(names, clause.Value)
		}
	}
	if len(names) == 0 {
		return d.openedTaskWriter(in)
	}
	if len(names) != 1 {
		return "", false
	}
	if agentID, ok := d.openedForAgent(in, names[0]); ok {
		return agentID, true
	}

	var ids []string
	for _, item := range slices.Concat(in.Listing.Agents, in.Listing.ArchivedAgents) {
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
func (d *Screen) openedForAgent(in screen.Inputs, name string) (string, bool) {
	agentID := d.agentID
	if agentID == "" || !strings.EqualFold(findAgentName(in.Listing, agentID), name) {
		return "", false
	}

	return agentID, true
}

// openedTaskWriter is the writer of the task the documents were opened for, while the filter still
// names that task.
func (d *Screen) openedTaskWriter(in screen.Inputs) (string, bool) {
	for _, clause := range in.Room.Pattern.Clauses {
		if clause.Key != searchsyntax.KeyTask || clause.Negated {
			continue
		}
		if _, ok := d.openedForTask(in, clause.Value); ok && d.agentID != "" {
			return d.agentID, true
		}
	}

	return "", false
}

// openedForTask is the task the documents were opened for, when the slug given is its slug.
func (d *Screen) openedForTask(in screen.Inputs, slug string) (string, bool) {
	taskID := d.taskID
	task, ok := in.Listing.Tasks[taskID]
	if taskID == "" || !ok || !strings.EqualFold(task.Slug, slug) {
		return "", false
	}

	return taskID, true
}

// searchSaid asks the server which documents hold the text as they stand now, with the passages it was found in keyed by document id.
func searchSaid(ctx context.Context, client api.AgentReads, said string) (map[string][]list.SaidHit, error) {
	resp, err := client.SearchDocuments(ctx, connect.NewRequest(&agentv1.SearchDocumentsRequest{
		Query: list.Phrase(said),
		Limit: list.SaysLimit,
	}))
	if err != nil {
		return nil, err
	}

	hits := map[string][]list.SaidHit{}
	for _, hit := range slices.Concat(resp.Msg.GetMatches(), resp.Msg.GetNear()) {
		hits[hit.GetDocumentId()] = append(hits[hit.GetDocumentId()], list.SaidHit{Snippet: hit.GetSnippet()})
	}

	return hits, nil
}
