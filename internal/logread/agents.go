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

// NewestAboutAgents is the last limit lines about any of the agents, oldest first.
func (r *Reader) NewestAboutAgents(ctx context.Context, agentIDs []string, limit int) (*Window, error) {
	return r.newest(ctx, aboutAny(agentIDs), limit)
}

// AboutAgentsSince is the lines about any of the agents written after offset, oldest first.
func (r *Reader) AboutAgentsSince(ctx context.Context, agentIDs []string, offset int64, limit int) (*Window, error) {
	return r.since(ctx, aboutAny(agentIDs), offset, limit)
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
