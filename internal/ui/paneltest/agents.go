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
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"
	worktreev1 "komrad/proto/gen/worktree/v1"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
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

// OpenArchivedAgents starts a panel on the archived agents over rows.
func OpenArchivedAgents(t *testing.T, rows ...proto.Message) *Panel {
	t.Helper()

	harness := Start(t, Params{})
	harness.Press("F", "tab", "esc")
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

// AnswerResume answers each resume with agent a1 back in pane %9.
func (p *Panel) AnswerResume() {
	p.Answer(agentv1connect.AgentServiceResumeAgentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&agentv1.ResumeAgentResponse{Agent: &agentv1.Agent{Id: "a1", PaneId: "%9"}}), nil
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

// HarnessFrames reads every pane as showing its harness while IsShowing holds.
type HarnessFrames struct {
	IsShowing bool
}

// DoesFrameShowHarness reports IsShowing whatever the pane.
func (f *HarnessFrames) DoesFrameShowHarness(string, string) bool {
	return f.IsShowing
}

// AnswerAgentCreate answers each agent create with agent a2 in the pane given.
func (p *Panel) AnswerAgentCreate(pane string) {
	p.Answer(agentv1connect.AgentServiceCreateAgentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&agentv1.CreateAgentResponse{Agent: &agentv1.Agent{Id: "a2", PaneId: pane}}), nil
	})
}

// AnswerWorkflowCreate answers each workflow create with workflow w2.
func (p *Panel) AnswerWorkflowCreate() {
	p.Answer(workflowv1connect.WorkflowServiceCreateWorkflowProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&workflowv1.CreateWorkflowResponse{Workflow: &workflowv1.Workflow{Id: "w2"}}), nil
	})
}

// AnswerDocuments lists the documents and reads the first of them whichever one is asked for.
func (p *Panel) AnswerDocuments(documents []*agentv1.Document) {
	read := proto.CloneOf(documents[0])
	read.Body = "# Design the cache\n\nKeep the cache warm."

	p.Answer(agentv1connect.AgentServiceListDocumentsProcedure, AnswerWith(&agentv1.ListDocumentsResponse{Documents: documents}))
	p.Answer(agentv1connect.AgentServiceGetDocumentProcedure, AnswerWith(&agentv1.GetDocumentResponse{Document: read}))
}

// IgnoringEventPrompts compares a workflow create without its event prompts.
var IgnoringEventPrompts = []cmp.Option{protocmp.IgnoreFields(&workflowv1.CreateWorkflowRequest{}, "event_prompts")}
