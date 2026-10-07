package ui

import (
	"slices"
	"strings"
	"time"

	"komrad/internal/searchsyntax"
)

var documentsQuery = queryGrammar{syntax: searchsyntax.Documents, check: checkDocumentsClause}

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
// and workflow:.
func (m *model) documentRowFits(r documentRow) clauseFits {
	return func(clause searchsyntax.Clause) bool {
		item := r.item
		switch clause.Key {
		case searchsyntax.KeyAgent:
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
		}

		return false
	}
}

// agentClause is the qualifier that picks out one agent's documents by its name.
func agentClause(name string) string {
	if strings.ContainsFunc(name, func(r rune) bool { return r == ' ' || r == '\t' }) {
		return searchsyntax.KeyAgent + `:"` + name + `"`
	}

	return searchsyntax.KeyAgent + ":" + name
}

// namedAgentID is the agent the filter's one agent: qualifier names exactly, which a new document
// is written for, and none when the filter names no single agent.
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

// filterDocumentsOf narrows the list to one agent's documents, as if its agent: qualifier were
// typed and applied.
func (m *model) filterDocumentsOf(agentID string) {
	m.filter.open(withGrammar(documentsQuery))
	m.filter.mode = filterModeFilter
	m.filter.openedIn = sectionList
	m.filter.setQuery(agentClause(findAgentName(m, agentID)))
	m.applyFilter()
}
