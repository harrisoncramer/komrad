package ui

import (
	"context"
	"slices"
	"strings"
	"time"

	"komrad/internal/searchsyntax"
	agentv1 "komrad/proto/gen/agent/v1"

	"connectrpc.com/connect"
)

var documentsQuery = queryGrammar{
	syntax: searchsyntax.Documents,
	check:  checkDocumentsClause,
	search: searchDocumentsSaid,
}

func (d *documentsScreen) queryGrammar() (queryGrammar, bool) {
	return documentsQuery, d.kind == documentKindDocuments
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
// and workflow:, and by what the server found it saying for says:.
func (m *model) documentRowFits(r documentRow) clauseFits {
	return func(clause searchsyntax.Clause) bool {
		item := r.item
		switch clause.Key {
		case searchsyntax.KeyAgent:
			if agentID, ok := m.openedForAgent(clause.Value); ok {
				return item.agentID == agentID
			}

			return containsFold(findAgentName(m, item.agentID), clause.Value)
		case searchsyntax.KeyWorkflow:
			agent, ok := m.listing.anyAgent(item.agentID)

			return ok && containsFold(m.listing.workflowName(agent.Agent.WorkflowID), clause.Value)
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

// agentClause is the qualifier that picks out one agent's documents by its name.
func agentClause(name string) string {
	if strings.ContainsAny(name, " \t") {
		return searchsyntax.KeyAgent + `:"` + name + `"`
	}

	return searchsyntax.KeyAgent + ":" + name
}

// namedAgentID is the one agent the filter's agent: qualifier names exactly, if any.
func (m *model) namedAgentID() (string, bool) {
	var names []string
	for _, clause := range m.filter.qualifiers.clauses {
		if clause.Key == searchsyntax.KeyAgent && !clause.Negated {
			names = append(names, clause.Value)
		}
	}
	if len(names) != 1 {
		return "", false
	}
	if agentID, ok := m.openedForAgent(names[0]); ok {
		return agentID, true
	}

	var ids []string
	for _, item := range slices.Concat(m.listing.agents, m.listing.archivedAgents) {
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
func (m *model) openedForAgent(name string) (string, bool) {
	agentID := m.documents.agentID
	if agentID == "" || !strings.EqualFold(findAgentName(m, agentID), name) {
		return "", false
	}

	return agentID, true
}

// filterDocumentsOf narrows the list to one agent's documents, as if its agent: qualifier were
// typed and applied.
func (m *model) filterDocumentsOf(agentID string) {
	m.filter.open(withGrammar(documentsQuery))
	m.filter.mode = filterModeFilter
	m.filter.openedIn = sectionList
	m.filter.setQuery(agentClause(findAgentName(m, agentID)))
	m.applyFilter()
}

// searchDocumentsSaid asks the server which documents hold the text as they stand now, with the passages it was found in keyed by document id.
func searchDocumentsSaid(ctx context.Context, client agentReads, text string) (map[string][]saidHit, error) {
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
