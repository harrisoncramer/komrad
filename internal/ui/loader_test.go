package ui_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/loader"
	"komrad/internal/ui/paneltest"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestLoaderStopsTicking covers the loader tick going quiet once no rendered row is loading.
func TestLoaderStopsTicking(t *testing.T) {
	tests := []struct {
		name  string
		open  func(t *testing.T) *paneltest.Panel
		leave func(harness *paneltest.Panel)
	}{
		{
			name: "nothing loading never starts it",
			open: openAgentsWithPullRequest(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN),
		},
		{
			name: "checks finishing stop it",
			open: openAgentsWithPullRequest(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING),
			leave: func(harness *paneltest.Panel) {
				harness.Watch(paneltest.Puts(newPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN)...))
			},
		},
		{
			name: "leaving the agents list stops it",
			open: openAgentsWithPullRequest(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING),
			leave: func(harness *paneltest.Panel) {
				harness.Press("tab")
			},
		},
		{
			name: "filtering the row out stops it",
			open: openAgentsWithPullRequest(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING),
			leave: func(harness *paneltest.Panel) {
				harness.Press("/", "tab", "z", "q", "x", "enter")
			},
		},
		{
			name: "switching the archive to its schedules stops it",
			open: openArchiveWithChecksRunning,
			leave: func(harness *paneltest.Panel) {
				harness.Press("F", "tab", "esc")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := tt.open(t)

			if tt.leave != nil {
				require.True(t, harness.IsSpinning(), "the loader never started")
				tt.leave(harness)
				harness.Advance(loader.Interval)
			}

			assert.False(t, harness.IsSpinning())
		})
	}
}

// TestLoaderSpinsWhileAWriteIsInFlight covers a held form's notice rendering the configured loader a frame a tick until its write lands.
func TestLoaderSpinsWhileAWriteIsInFlight(t *testing.T) {
	create := workflowv1connect.WorkflowServiceCreateWorkflowProcedure
	harness := paneltest.Start(t, paneltest.Params{
		Width:  160,
		Height: 40,
		Loader: loader.Arcs,
	})
	harness.AnswerWorktreeReads()
	harness.Watch(paneltest.Snapshot(newFormWorkflow()))
	harness.Answer(create, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&workflowv1.CreateWorkflowResponse{Workflow: &workflowv1.Workflow{Id: "w2"}}), nil
	})
	harness.Hold(create)
	harness.Press(slices.Concat([]string{"space", "w"}, strings.Split("web", ""), []string{"enter"})...)

	var rendered []string
	for range 3 {
		rendered = append(rendered, readWriteFrame(t, harness.Frame(), "creating workflow web…"))
		require.True(t, harness.IsSpinning(), "the loader stopped ticking with the write in flight")
	}

	assert.Equal(t, []string{"◜", "◝", "◞"}, rendered)

	harness.Release(create)
	harness.Watch(paneltest.Puts(paneltest.Workflow("w2", "web")))
	harness.Advance(loader.Interval)
	assert.False(t, harness.IsSpinning(), "the loader goes quiet once the write lands")
}

func openAgentsWithPullRequest(state pullrequestv1.PullRequestState) func(t *testing.T) *paneltest.Panel {
	return func(t *testing.T) *paneltest.Panel {
		t.Helper()

		return openNoticed(t, paneltest.Params{Loader: loader.Arcs}, newPullRequested(state)...)
	}
}

func openArchiveWithChecksRunning(t *testing.T) *paneltest.Panel {
	t.Helper()

	rows := newPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING)
	agent, ok := rows[0].(*agentv1.Agent)
	require.True(t, ok)
	agent.ArchivedAt = paneltest.ArchivedAt

	return paneltest.OpenArchivedAgents(t, append([]proto.Message{paneltest.Workflow("w1", "backend")}, rows...)...)
}

func readWriteFrame(t *testing.T, frame, doing string) string {
	t.Helper()

	for _, line := range paneltest.SplitLines(ansi.Strip(frame)) {
		at := strings.Index(line, doing)
		if at < 0 {
			continue
		}

		fields := strings.Fields(line[:at])
		require.NotEmpty(t, fields)

		return fields[len(fields)-1]
	}

	require.Fail(t, "nothing is rendered doing "+doing)

	return ""
}

func openNoticed(t *testing.T, params paneltest.Params, rows ...proto.Message) *paneltest.Panel {
	t.Helper()

	harness := paneltest.Start(t, params)
	harness.AnswerWorktreeReads()
	harness.Watch(paneltest.Snapshot(append([]proto.Message{paneltest.Workflow("w1", "backend")}, rows...)...))
	harness.Press("g", "g", "j")

	return harness
}

func newNoticed() *agentv1.Agent {
	return &agentv1.Agent{
		Id:         "n1",
		Name:       "worker",
		WorkflowId: "w1",
		Cwd:        "/repo",
		State:      agentv1.AgentState_AGENT_STATE_WAITING,
	}
}

func newPullRequested(state pullrequestv1.PullRequestState) []proto.Message {
	agent := newNoticed()
	task := paneltest.TaskOf(agent)
	task.RepositoryUrl = xptr.Ptr(repositoryURL)
	task.Branch = xptr.Ptr("agent/n1")

	return []proto.Message{
		agent,
		&pullrequestv1.Repository{Url: repositoryURL},
		&pullrequestv1.PullRequest{
			Id:                "pr-1",
			RepositoryUrl:     repositoryURL,
			HeadRepositoryUrl: repositoryURL,
			HeadBranch:        "agent/n1",
			Number:            7,
			State:             state,
			OpenedAt:          timestamppb.New(time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)),
		},
		task,
	}
}
