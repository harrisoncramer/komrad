package ui_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"

	"komrad/internal/ui"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	taskv1 "komrad/proto/gen/task/v1"
)

// TestNotices covers the record spelling out the marks on the hovered row above its fields.
func TestNotices(t *testing.T) {
	t.Run("a row carrying no mark says nothing", func(t *testing.T) {
		harness := openNoticed(t, panelHarnessParams{}, newNoticed())

		assert.Empty(t, readNoticeBlock(harness))
	})
}

// TestNoticesGolden renders the notice block whole, glyphs and colours included.
func TestNoticesGolden(t *testing.T) {
	tests := []struct {
		name  string
		width int
		press []string
		rows  []proto.Message
	}{
		{
			name:  "marks-beside-the-name",
			press: widenListPresses,
			rows:  []proto.Message{newHeld(newFailedRun()), newUnreadMail("n1")},
		},
		{
			name:  "column-facts",
			press: widenListPresses,
			rows: append(newRebasing(),
				&agentv1.WatchedReview{
					Id:              "v1",
					TaskId:          "task-n1",
					SnapshotId:      "n1-snapshot-1",
					SnapshotOrdinal: 1,
					ReleasedAt:      "2026-09-20T12:00:00Z",
					Verdict:         agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
				},
			),
		},
		{
			name:  "a-conflicting-pull-request",
			press: widenListPresses,
			rows:  newPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING),
		},
		{
			name:  "a-failing-pull-request",
			press: widenListPresses,
			rows:  newPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING),
		},
		{
			name: "a-failed-create",
			rows: newFailedCreate(),
		},
		{
			name:  "a-wrapped-sentence",
			width: 34,
			rows:  []proto.Message{newHeld(newNoticed())},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openNoticed(t, panelHarnessParams{width: tt.width}, tt.rows...)
			harness.press(tt.press...)

			ui.RequireGolden(t, "notices-"+tt.name, strings.Join(readNoticeBlock(harness), "\n"))
		})
	}
}

func openNoticed(t *testing.T, params panelHarnessParams, rows ...proto.Message) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, params)
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(append([]proto.Message{newLiveWorkflow("w1", "backend")}, rows...)...))
	harness.press("g", "g", "j")

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
	task := newTaskOf(agent)
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
			OpenedAt:          "2026-09-19T12:00:00Z",
		},
		task,
	}
}

func newFailedRun() *agentv1.Agent {
	agent := newNoticed()
	agent.FailedRun = true

	return agent
}

func newHeld(agent *agentv1.Agent) *agentv1.Agent {
	agent.HeldAt = "1970-01-01T00:00:00Z"

	return agent
}

func newRebasing() []proto.Message {
	agent := newNoticed()
	task := newTaskOf(agent)
	task.Sequence = taskv1.Sequence_SEQUENCE_REBASE

	return []proto.Message{agent, task}
}

func newFailedCreate() []proto.Message {
	agent := newNoticed()
	agent.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE

	return []proto.Message{agent}
}

func findRule(frame string) int {
	return slices.IndexFunc(splitLines(frame), func(line string) bool {
		return strings.HasPrefix(line, "─")
	})
}

func readNoticeBlock(harness *panelHarness) []string {
	styled := splitLines(harness.readRawFrame())

	var block []string
	width, rules := 0, 0
	for i, line := range splitLines(harness.readFrame()) {
		if strings.HasPrefix(line, "─") {
			rules++
			width = ruleWidth(line)

			continue
		}
		if rules != 1 {
			continue
		}
		if field := strings.TrimLeft(line, "› "); strings.HasPrefix(field, "agent ") || strings.HasPrefix(field, "directory ") {
			break
		}

		if strings.TrimSpace(ansi.Truncate(line, width, "")) != "" {
			block = append(block, strings.TrimRight(cutStyled(styled[i], width), " "))
		}
	}

	return block
}

func newUnreadMail(agentID string) *agentv1.Mail {
	return &agentv1.Mail{
		Id:        "m1",
		ToAgentId: agentID,
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
}
