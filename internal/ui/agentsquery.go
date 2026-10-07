package ui

import (
	"strconv"
	"strings"
	"time"

	"komrad/internal/searchsyntax"
	"komrad/internal/ui/panel"
)

var agentsQuery = queryGrammar{syntax: searchsyntax.Agents, check: checkAgentsClause}

func (a *agentsScreen) queryGrammar() queryGrammar {
	return agentsQuery
}

func checkAgentsClause(clause searchsyntax.Clause, now time.Time) error {
	switch clause.Key {
	case searchsyntax.KeyIs:
		switch strings.ToLower(clause.Value) {
		case "implementer", "reviewer", "orchestrator":
			return nil
		}

		return searchsyntax.ErrBadQualifier.Saying("is:%s is not one of implementer, reviewer or orchestrator", clause.Value)
	case searchsyntax.KeySince:
		_, err := searchsyntax.ParseStart(clause.Key, clause.Value, now)

		return err
	case searchsyntax.KeyUntil:
		_, err := searchsyntax.ParseEnd(clause.Key, clause.Value, now)

		return err
	case searchsyntax.KeyPR:
		if _, err := strconv.Atoi(clause.Value); err != nil {
			return searchsyntax.ErrBadQualifier.Saying("pr:%s is not a number", clause.Value)
		}
	}

	return nil
}

// agentRowFits is how a row of the tree meets a qualifier: a header by its workflow's name or the
// orchestrator behind it, an agent by its own.
func (m *model) agentRowFits(r agentRow) clauseFits {
	return func(clause searchsyntax.Clause) bool {
		if !r.header {
			return m.agentFits(r.item, clause)
		}
		if clause.Key == searchsyntax.KeyWorkflow {
			return containsFold(r.workflow.Name, clause.Value)
		}
		if r.orchestrator == nil {
			return false
		}

		return m.agentFits(*r.orchestrator, clause)
	}
}

func (m *model) agentFits(row panel.AgentRow, clause searchsyntax.Clause) bool {
	agent := row.Agent
	switch clause.Key {
	case searchsyntax.KeyAgent:
		return containsFold(agent.Name, clause.Value)
	case searchsyntax.KeyWorkflow:
		return containsFold(m.listing.workflowName(agent.WorkflowID), clause.Value)
	case searchsyntax.KeyIs:
		return strings.EqualFold(string(agent.Role), clause.Value)
	case searchsyntax.KeyHarness:
		return strings.EqualFold(agent.Harness, clause.Value)
	case searchsyntax.KeyPR:
		number, err := strconv.Atoi(clause.Value)
		if err != nil {
			return false
		}
		for _, pr := range row.PullRequests {
			if int(pr.Number) == number {
				return true
			}
		}

		return false
	case searchsyntax.KeySince:
		at, err := searchsyntax.ParseStart(clause.Key, clause.Value, m.filter.compiledAt)

		return err == nil && !agent.CreatedAt.Before(at)
	case searchsyntax.KeyUntil:
		at, err := searchsyntax.ParseEnd(clause.Key, clause.Value, m.filter.compiledAt)

		return err == nil && agent.CreatedAt.Before(at)
	case searchsyntax.KeySays:
		return m.filter.says(agent.ID)
	}

	return false
}

// pickedAgentRows is the keys of the rows the filter's qualifiers pick, nil when it names none.
func (m *model) pickedAgentRows(rows []agentRow) map[string]bool {
	if len(m.filter.clauses) == 0 {
		return nil
	}

	picked := map[string]bool{}
	for _, r := range rows {
		if m.filter.holds(m.agentRowFits(r)) {
			picked[rowKey(r)] = true
		}
	}

	return picked
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
