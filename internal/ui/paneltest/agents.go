package paneltest

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	worktreev1 "komrad/proto/gen/worktree/v1"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

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
