package paneltest

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	worktreev1 "komrad/proto/gen/worktree/v1"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// WidenListPresses drags the list's border out as far as it goes.
var WidenListPresses = slices.Repeat([]string{"ctrl+space", "l"}, 10)

// OpenAgents starts a panel on the agents list over rows.
func OpenAgents(t *testing.T, rows ...proto.Message) *Panel {
	t.Helper()

	return OpenAgentsWith(t, Params{}, rows...)
}

// OpenAgentsWith starts a panel built over params on the agents list over rows.
func OpenAgentsWith(t *testing.T, params Params, rows ...proto.Message) *Panel {
	t.Helper()

	harness := Start(t, params)
	harness.AnswerWorktreeReads()
	harness.Watch(Snapshot(rows...))

	return harness
}

// AnswerWorktreeReads answers each worktree read with a worktree at the directory asked for.
func (p *Panel) AnswerWorktreeReads() {
	p.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		read, ok := req.Any().(*worktreev1.GetWorktreeRequest)
		if !ok {
			return nil, fmt.Errorf("%s sent a %T", req.Spec().Procedure, req.Any())
		}

		return connect.NewResponse(&worktreev1.GetWorktreeResponse{Worktree: &worktreev1.Worktree{Dir: read.GetDir()}}), nil
	})
}

// AnswerLastScreens answers each agent's saved screen at 80x24, and not found for any other.
func (p *Panel) AnswerLastScreens(screens map[string]string) {
	p.Answer(agentv1connect.AgentServiceGetLastScreenProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		read, ok := req.Any().(*agentv1.GetLastScreenRequest)
		if !ok {
			return nil, fmt.Errorf("%s sent a %T", req.Spec().Procedure, req.Any())
		}

		frame, ok := screens[read.GetAgentId()]
		if !ok {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("no saved screen"))
		}

		return connect.NewResponse(&agentv1.GetLastScreenResponse{
			Frame:      frame,
			Width:      80,
			Height:     24,
			CapturedAt: timestamppb.New(time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)),
		}), nil
	})
}

// MoveToRow puts the list's cursor on the row named the one given.
func (p *Panel) MoveToRow(name string) {
	p.t.Helper()

	p.Press("g", "g")
	for range 12 {
		if slices.Contains(strings.Fields(HoveredRow(p.Frame())), name) {
			return
		}
		p.Press("j")
	}

	require.Contains(p.t, strings.Fields(HoveredRow(p.Frame())), name, "no row is named %q", name)
}
