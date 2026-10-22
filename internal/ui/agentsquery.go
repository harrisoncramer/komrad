package ui

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"komrad/internal/searchsyntax"
	"komrad/internal/ui/api"
	"komrad/internal/ui/panel"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	paginationv1 "komrad/proto/gen/pagination/v1"

	"connectrpc.com/connect"
)

// agentsQueryOf is the agents lists' grammar, its says: qualifier searching the transcripts the client reads.
func agentsQueryOf(agents api.AgentReads) queryGrammar {
	return queryGrammar{
		syntax: searchsyntax.Agents,
		check:  checkAgentsClause,
		search: func(ctx context.Context, text string) (map[string][]saidHit, error) {
			return searchTranscriptsSaid(ctx, agents, text)
		},
	}
}

func (a *agentsScreen) queryGrammar() (queryGrammar, bool) {
	return agentsQueryOf(a.deps.clients.Agents), true
}

func checkAgentsClause(clause searchsyntax.Clause, now time.Time) error {
	switch clause.Key {
	case searchsyntax.KeyIs:
		switch strings.ToLower(clause.Value) {
		case "writer", "reviewer", "orchestrator":
			return nil
		}

		return searchsyntax.ErrBadQualifier.Saying("is:%s is not one of writer, reviewer or orchestrator", clause.Value)
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

// agentRowFits matches a row of the tree against a qualifier, a header standing for its workflow.
func (m *model) agentRowFits(l panel.Listing) func(agentRow) clauseFits {
	return func(r agentRow) clauseFits {
		return func(clause searchsyntax.Clause) bool {
			if !r.header {
				return m.agentFits(l, r.item, clause)
			}
			if r.isTask() {
				writer, ok := r.writer()

				return clause.Key == searchsyntax.KeyTask && containsFold(r.task.Slug, clause.Value) ||
					ok && m.agentFits(l, writer, clause)
			}
			if clause.Key == searchsyntax.KeyWorkflow {
				return containsFold(r.workflow.Name, clause.Value)
			}
			if r.orchestrator == nil {
				return false
			}

			return m.agentFits(l, *r.orchestrator, clause)
		}
	}
}

func (m *model) agentFits(l panel.Listing, row panel.AgentRow, clause searchsyntax.Clause) bool {
	agent := row.Agent
	switch clause.Key {
	case searchsyntax.KeyAgent:
		return containsFold(agent.Name, clause.Value)
	case searchsyntax.KeyWorkflow:
		return containsFold(l.WorkflowName(agent.WorkflowID), clause.Value)
	case searchsyntax.KeyIs:
		return strings.EqualFold(string(agent.Role), clause.Value)
	case searchsyntax.KeyTask:
		return containsFold(l.Tasks.FindForAgent(agent).Slug, clause.Value)
	case searchsyntax.KeyHarness:
		return strings.EqualFold(agent.Harness, clause.Value)
	case searchsyntax.KeyPR:
		number, err := strconv.Atoi(clause.Value)
		if err != nil {
			return false
		}

		return slices.ContainsFunc(l.Tasks.FindForAgent(agent).PullRequests, func(pr panel.PullRequest) bool { return int(pr.Number) == number })
	case searchsyntax.KeySince:
		at, err := searchsyntax.ParseStart(clause.Key, clause.Value, m.filter.qualifiers.compiledAt)

		return err == nil && !agent.CreatedAt.Before(at)
	case searchsyntax.KeyUntil:
		at, err := searchsyntax.ParseEnd(clause.Key, clause.Value, m.filter.qualifiers.compiledAt)

		return err == nil && agent.CreatedAt.Before(at)
	case searchsyntax.KeySays:
		return m.filter.hasSaid(agent.ID)
	}

	return false
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

func (a *archivedAgents) queryGrammar() (queryGrammar, bool) {
	return agentsQueryOf(a.deps.clients.Agents), true
}

// searchTranscriptsSaid asks the server which agents' transcripts hold the text, with the turns it was found in keyed by agent id.
func searchTranscriptsSaid(ctx context.Context, client api.AgentReads, text string) (map[string][]saidHit, error) {
	resp, err := client.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
		Query:      phrase(text),
		Pagination: &paginationv1.Pagination{Limit: xptr.Ptr[int32](saysLimit)},
	}))
	if err != nil {
		return nil, err
	}

	hits := map[string][]saidHit{}
	for _, hit := range slices.Concat(resp.Msg.GetMatches(), resp.Msg.GetNear()) {
		hits[hit.GetAgentId()] = append(hits[hit.GetAgentId()], saidHit{
			snippet: hit.GetSnippet(),
			offset:  hit.GetContentOffset(),
			length:  hit.GetContentLength(),
		})
	}

	return hits, nil
}
