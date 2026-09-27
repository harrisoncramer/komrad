package ui_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"

	"komrad/internal/ui"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// TestNotices covers the record spelling out the marks on the hovered row above its fields.
func TestNotices(t *testing.T) {
	t.Run("every mark the row carries is said above its fields", func(t *testing.T) {
		harness := openNoticed(t, panelHarnessParams{}, newMarked(), newUnreadMail("n1"))

		record := readRecordProse(harness.readFrame())
		assert.Contains(t, record, "a script komrad fired failed, log unread")
		assert.Contains(t, record, "mail waiting, it has not read it")
		assert.Contains(t, record, "its directory no longer exists")
		assert.Equal(t, "worker", readRecordField(harness.readFrame(), "name"))
	})

	t.Run("a row carrying no mark says nothing", func(t *testing.T) {
		harness := openNoticed(t, panelHarnessParams{}, newNoticed())

		assert.Empty(t, readNoticeBlock(harness))
	})

	t.Run("the notices take their lines from the fields rather than the list", func(t *testing.T) {
		plain := openNoticed(t, panelHarnessParams{}, newNoticed())
		marked := openNoticed(t, panelHarnessParams{}, newMarked(), newUnreadMail("n1"))

		assert.Len(t, readRecord(marked.readFrame()), len(readRecord(plain.readFrame())))
		assert.Equal(t, findRule(plain.readFrame()), findRule(marked.readFrame()), "the record starts on the same line")
	})

	tests := []struct {
		name       string
		params     panelHarnessParams
		rows       []proto.Message
		want       []string
		wantAbsent []string
	}{
		{
			name: "a short pane gives up the last notice before the fields",
			params: panelHarnessParams{
				width:  40,
				height: 13,
			},
			rows:       []proto.Message{newMarked(), newUnreadMail("n1")},
			want:       []string{"name", "a script komrad fired failed"},
			wantAbsent: []string{"no longer exists"},
		},
		{
			name: "a pane too narrow for one whole sentence cuts it short and says so",
			params: panelHarnessParams{
				width:  24,
				height: 13,
			},
			rows:       []proto.Message{newFailedRun()},
			want:       []string{"a script komrad", "…"},
			wantAbsent: []string{"log unread"},
		},
		{
			name: "the record keeps a row to count the fields it left out",
			params: panelHarnessParams{
				width:  40,
				height: 20,
			},
			rows: []proto.Message{newFailedRun()},
			want: []string{"name", "more"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openNoticed(t, tt.params, tt.rows...)

			record := readRecordProse(harness.readFrame())
			for _, want := range tt.want {
				assert.Contains(t, record, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, record, absent)
			}
		})
	}
}

// TestNoticesGolden draws the notice block whole, glyphs and colours included.
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
			rows: []proto.Message{
				newRebasing(),
				&agentv1.WatchedReview{
					Id:           "v1",
					AgentId:      "n1",
					RoundId:      "n1-round-1",
					RoundOrdinal: 1,
					ReleasedAt:   "2026-09-20T12:00:00Z",
					Verdict:      agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
				},
			},
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
			rows: []proto.Message{newFailedCreate()},
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
	harness.press("j")

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
	agent.RepositoryUrl = proto.String(repositoryURL)
	agent.Branch = proto.String("agent/n1")

	return []proto.Message{
		agent,
		&pullrequestv1.Repository{
			Url:  repositoryURL,
			Read: true,
		},
		&pullrequestv1.PullRequest{
			Id:                "pr-1",
			RepositoryUrl:     repositoryURL,
			HeadRepositoryUrl: repositoryURL,
			HeadBranch:        "agent/n1",
			Number:            7,
			State:             state,
			OpenedAt:          "2026-09-19T12:00:00Z",
		},
	}
}

func newMarked() *agentv1.Agent {
	agent := newFailedRun()
	agent.WorktreeRemoved = true

	return agent
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

func newRebasing() *agentv1.Agent {
	agent := newNoticed()
	agent.SequenceInProgress = true

	return agent
}

func newFailedCreate() *agentv1.Agent {
	agent := newNoticed()
	agent.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE
	agent.WorktreeRemoved = true

	return agent
}

func findRule(frame string) int {
	return slices.IndexFunc(splitLines(frame), func(line string) bool {
		return strings.HasPrefix(line, "─")
	})
}

func readRecordProse(frame string) string {
	return joinProse(readRecord(frame))
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
		if strings.HasPrefix(strings.TrimLeft(line, "› "), "name ") {
			break
		}

		if strings.TrimSpace(ansi.Truncate(line, width, "")) != "" {
			block = append(block, strings.TrimRight(cutStyled(styled[i], width), " "))
		}
	}

	return block
}
