package logread

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"

	"komrad/internal/logging"
)

// agentKeys are the fields a line names an agent in.
var agentKeys = []string{logging.AgentID, logging.TargetAgentID, logging.ScriptAgentID}

// NewestAboutAgents is the newest lines about any of the agents that the query asks for, oldest first.
func (r *Reader) NewestAboutAgents(ctx context.Context, agentIDs []string, query Query) (*Window, error) {
	return r.newest(ctx, matching(query.Pattern, aboutAny(agentIDs)), query.Limit)
}

// AboutAgentsSince is the lines about any of the agents written after offset that the query asks for, oldest first.
func (r *Reader) AboutAgentsSince(ctx context.Context, agentIDs []string, offset int64, query Query) (*Window, error) {
	return r.since(ctx, matching(query.Pattern, aboutAny(agentIDs)), offset, query.Limit)
}

// aboutAny keeps a line that names any of the agents in one of the fields that name an agent.
func aboutAny(agentIDs []string) func([]byte) bool {
	return func(line []byte) bool {
		if !slices.ContainsFunc(agentIDs, func(id string) bool { return bytes.Contains(line, []byte(id)) }) {
			return false
		}

		var fields map[string]any
		if err := json.Unmarshal(line, &fields); err != nil {
			return false
		}

		for _, key := range agentKeys {
			if id, ok := fields[key].(string); ok && slices.Contains(agentIDs, id) {
				return true
			}
		}

		return false
	}
}
