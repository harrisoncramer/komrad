package ui

import (
	"context"
	"testing"

	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestIsMarkLetter pins what may be a mark.
func TestIsMarkLetter(t *testing.T) {
	for _, key := range []string{"a", "m", "z"} {
		assert.True(t, isMarkLetter(key), "key %q", key)
	}

	for _, key := range []string{"A", "Z", "1", "esc", "ctrl+a", "'", "", "ab", "●"} {
		assert.False(t, isMarkLetter(key), "key %q", key)
	}
}

func markTree() model {
	m := selectionTree()
	m.api = api{agents: &fakeMarkAgents{}}

	return m
}

func markRow(m *model, id string) {
	if m.listing.marks == nil {
		m.listing.marks = map[string]string{}
	}
	m.listing.marks[id] = "c"
}

func pressMark(t *testing.T, m model, keys ...string) model {
	t.Helper()

	var next tea.Model = m
	for _, key := range keys {
		updated, _ := next.(model).Update(keyMsg(key))
		next = updated
	}

	after, ok := next.(model)
	require.True(t, ok)

	return after
}

type fakeMarkAgents struct {
	agentv1connect.AgentServiceClient
	id         string
	workflowID string
	mark       *string
}

func (f *fakeMarkAgents) SetMark(_ context.Context, req *connect.Request[agentv1.SetMarkRequest]) (*connect.Response[emptypb.Empty], error) {
	f.id = req.Msg.GetAgentId()
	f.workflowID = req.Msg.GetWorkflowId()
	f.mark = req.Msg.Mark

	return connect.NewResponse(&emptypb.Empty{}), nil
}
