// Package usage stores the token counts an agent's CLI reported for each exchange, one row per
// exchange so that a reading over the last few hours is summed rather than remembered.
//
// A lifetime total on the agent's own row would be cheaper and could never be windowed, and the
// question this answers is which agent is spending now rather than which has spent most since
// it started. Rows are keyed by whatever the log calls the exchange, so a log read twice is
// counted once, and swept once they are older than any window the panel offers.
package usage

import (
	"context"
	"fmt"
	"time"

	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"
)

// RecentWindow is how far back a reading looks, a few hours because the question it answers is
// which agent is spending now rather than which has spent most since it started.
const RecentWindow = 4 * time.Hour

// Retention is how far back rows are kept, wider than any window the panel draws so that a
// longer one can be offered without waiting for the sweep to catch up.
const Retention = 24 * time.Hour

// Reported is one exchange as the CLI described it, the counts kept apart because weave has no
// way to weigh a cache read against a fresh input token.
type Reported struct {
	Key        string
	At         time.Time
	Input      int64
	CacheRead  int64
	CacheWrite int64
	Output     int64
}

// Totals is what one agent's exchanges came to over a window.
type Totals struct {
	AgentID    string
	Input      int64
	CacheRead  int64
	CacheWrite int64
	Output     int64
}

// Tokens is every count added together, which is the whole traffic between the agent and the
// model rather than anything about a bill or a rate limit.
func (t Totals) Tokens() int64 {
	return t.Input + t.CacheRead + t.CacheWrite + t.Output
}

type Store struct {
	queries *models.Queries
}

// New builds a usage store over db, either the pool or a transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

// Append records what an agent's CLI reported, leaving alone any exchange already stored under
// the same key, which is what makes a re-read of a log harmless.
func (s *Store) Append(ctx context.Context, agentID string, reported []Reported) error {
	if !store.IsID(agentID) || len(reported) == 0 {
		return nil
	}

	params := models.InsertAgentTokenUsageParams{AgentID: agentID}
	for _, r := range reported {
		params.Keys = append(params.Keys, r.Key)
		params.Ats = append(params.Ats, r.At)
		params.Inputs = append(params.Inputs, r.Input)
		params.CacheReads = append(params.CacheReads, r.CacheRead)
		params.CacheWrites = append(params.CacheWrites, r.CacheWrite)
		params.Outputs = append(params.Outputs, r.Output)
	}

	if err := s.queries.InsertAgentTokenUsage(ctx, params); err != nil {
		return fmt.Errorf("failed to record the token usage of agent %q: %w", agentID, err)
	}

	return nil
}

// SumSince is what every agent that reported anything since a moment came to, agents that
// reported nothing in that span being absent rather than present holding zero.
func (s *Store) SumSince(ctx context.Context, since time.Time) ([]Totals, error) {
	rows, err := s.queries.SumAgentTokenUsageSince(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("failed to sum the token usage since %s: %w", since.Format(time.RFC3339), err)
	}

	return xslice.Map(rows, func(row *models.SumAgentTokenUsageSinceRow) Totals {
		return Totals{
			AgentID:    row.AgentID,
			Input:      row.InputTokens,
			CacheRead:  row.CacheReadTokens,
			CacheWrite: row.CacheWriteTokens,
			Output:     row.OutputTokens,
		}
	}), nil
}

// Prune drops every exchange older than Retention.
func (s *Store) Prune(ctx context.Context) error {
	if err := s.queries.PruneAgentTokenUsage(ctx, time.Now().Add(-Retention)); err != nil {
		return fmt.Errorf("failed to prune stored token usage: %w", err)
	}

	return nil
}
