package checks_test

import (
	"strings"
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/glyph"
	"komrad/internal/ui/paneltest"
	"komrad/lib/dates"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	"komrad/proto/gen/pullrequest/v1/pullrequestv1connect"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestStatusChecksList covers the status checks listed for the agent's pull request.
func TestStatusChecksList(t *testing.T) {
	t.Parallel()

	listed := answerStatusChecks(
		newStatusCheck("1", "lint", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "SUCCESS"),
		newStatusCheck("2", "test", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING, ""),
		newStatusCheck("3", "build", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_QUEUED, ""),
	)

	t.Run("it asks for the status checks of the agent's pull request", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, listed)

		harness.AssertSentTo(pullrequestv1connect.PullRequestServiceListStatusChecksProcedure, &pullrequestv1.ListStatusChecksRequest{PullRequestId: "pr-1"})
	})

	t.Run("each status check is a row", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, listed)
		harness.Press(everyStatusCheck...)

		frame := harness.Frame()
		assert.Equal(t, []string{"lint", "test", "build"}, paneltest.ListRows(frame))
	})

	t.Run("each status check is rendered with the icon the sidebar renders its pull request with", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, answerStatusChecks(
			newStatusCheck("1", "lint", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "SUCCESS"),
			newStatusCheck("2", "docs", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "SKIPPED"),
			newStatusCheck("3", "vet", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "FAILURE"),
			newStatusCheck("4", "test", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING, ""),
			newStatusCheck("5", "deploy", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "NEUTRAL"),
			newStatusCheck("6", "e2e", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_UNKNOWN, ""),
		))
		harness.Press(everyStatusCheck...)

		frame := harness.Frame()
		assert.Contains(t, frame, glyph.Defaults().Spell(glyph.StatusCheckPassing)+" lint")
		assert.Contains(t, frame, glyph.Defaults().Spell(glyph.StatusCheckSkipped)+" docs")
		assert.Contains(t, frame, glyph.Defaults().Spell(glyph.StatusCheckFailing)+" vet")
		assert.Contains(t, frame, "⠋ test")
		assert.Contains(t, frame, glyph.Defaults().Spell(glyph.StatusCheckSkipped)+" deploy")
		assert.Contains(t, frame, glyph.Defaults().Spell(glyph.StatusCheckUnknown)+" e2e")
		assert.NotContains(t, frame, "success")
	})

	t.Run("the spinner turns while a status check runs", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, listed)

		assert.True(t, harness.IsSpinning())
	})

	t.Run("the spinner rests once every status check has finished", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, answerStatusChecks(
			newStatusCheck("1", "lint", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "SUCCESS"),
		))

		assert.False(t, harness.IsSpinning())
	})

	t.Run("the cursor moves through the status checks", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, listed)
		harness.Press("j")

		assert.Contains(t, paneltest.HoveredRow(harness.Frame()), "build")
	})

	t.Run("searching lands on the status check it found", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, listed)
		harness.Press("/", "b", "u", "i", "enter")

		assert.Contains(t, paneltest.HoveredRow(harness.Frame()), "build")
	})

	t.Run("a pull request with no status checks says so", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, answerStatusChecks())

		assert.Contains(t, harness.Frame(), "no status checks yet")
	})

	t.Run("a refused listing says why", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, func(harness *paneltest.Panel) {
			harness.Refuse(pullrequestv1connect.PullRequestServiceListStatusChecksProcedure, connect.NewError(connect.CodeUnavailable, nil))
		})

		assert.Contains(t, harness.Frame(), "could not list")
	})
}

// TestStatusChecksScope covers the row of scopes narrowing the status checks listed.
func TestStatusChecksScope(t *testing.T) {
	t.Parallel()

	listed := answerStatusChecks(
		newStatusCheck("1", "lint", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "SUCCESS"),
		newStatusCheck("2", "test", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING, ""),
		newStatusCheck("3", "vet", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "FAILURE"),
		newStatusCheck("4", "docs", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "SKIPPED"),
		newStatusCheck("5", "build", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_QUEUED, ""),
		newStatusCheck("6", "e2e", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_UNKNOWN, ""),
	)

	tests := []struct {
		name  string
		press []string
		want  []string
	}{
		{
			name: "the listing opens on the status checks not passing while any are",
			want: []string{"test", "vet", "build", "e2e"},
		},
		{
			name:  "active keeps the status checks still to finish",
			press: []string{"F", "tab", "esc"},
			want:  []string{"test", "build"},
		},
		{
			name:  "failing keeps the finished status checks that neither passed nor were skipped",
			press: []string{"F", "tab", "tab", "esc"},
			want:  []string{"vet"},
		},
		{
			name:  "the row wraps back to every status check",
			press: []string{"F", "tab", "tab", "tab", "esc"},
			want:  []string{"lint", "test", "vet", "docs", "build", "e2e"},
		},
		{
			name:  "every status check is one step back",
			press: []string{"F", "shift+tab", "esc"},
			want:  []string{"lint", "test", "vet", "docs", "build", "e2e"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openStatusChecks(t, listed)
			harness.Press(tt.press...)

			assert.Equal(t, tt.want, paneltest.ListRows(harness.Frame()))
		})
	}

	t.Run("a status check both scopes list keeps the cursor", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, listed)
		harness.Press("j", "j", "F", "tab", "esc")

		assert.Contains(t, paneltest.HoveredRow(harness.Frame()), "build")
	})

	t.Run("the listing opens on every status check once all pass", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, answerStatusChecks(
			newStatusCheck("1", "lint", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "SUCCESS"),
			newStatusCheck("2", "docs", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "SKIPPED"),
		))

		assert.Equal(t, []string{"lint", "docs"}, paneltest.ListRows(harness.Frame()))
	})

	t.Run("a scope holding none of the status checks says why", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			press []string
			want  string
		}{
			{
				name:  "a scope that holds none of the status checks says so",
				press: []string{"F", "tab", "tab", "tab", "esc"},
				want:  "no failing status checks",
			},
			{
				name:  "not passing says every status check passes when none fail",
				press: []string{"F", "tab", "esc"},
				want:  "every status check passes",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				harness := openStatusChecks(t, answerStatusChecks(
					newStatusCheck("1", "lint", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "SUCCESS"),
				))
				harness.Press(tt.press...)

				assert.Contains(t, harness.Frame(), tt.want)
			})
		}
	})
}

// TestStatusChecksSteps covers the side pane rendering the steps of the hovered status check.
func TestStatusChecksSteps(t *testing.T) {
	t.Parallel()

	done := pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED

	lint := newStatusCheck("1", "lint", done, "SUCCESS")
	lint.StartedAt, lint.FinishedAt = timestamppb.New(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)), timestamppb.New(time.Date(2026, 10, 4, 12, 1, 12, 0, time.UTC))
	lint.Steps = []*pullrequestv1.StatusCheckStep{
		newStatusCheckStep("golangci-lint", done, "success", time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), time.Date(2026, 10, 4, 12, 1, 12, 0, time.UTC)),
	}

	test := newStatusCheck("2", "test", done, "FAILURE")
	test.Steps = []*pullrequestv1.StatusCheckStep{
		newStatusCheckStep("Checkout", done, "success", time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), time.Date(2026, 10, 4, 12, 0, 5, 0, time.UTC)),
		newStatusCheckStep("Run tests", done, "failure", time.Date(2026, 10, 4, 12, 0, 5, 0, time.UTC), time.Date(2026, 10, 4, 12, 3, 13, 0, time.UTC)),
	}

	status := newStatusCheck("3", "codecov", done, "SUCCESS")

	listed := answerStatusChecks(lint, test, status)

	t.Run("the hovered status check's steps are rendered with their icons and durations", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, listed)
		harness.Press(append(everyStatusCheck, "k")...)

		frame := harness.Frame()
		assert.Contains(t, frame, "1m 12s · 1 step")
		assert.Regexp(t, glyph.Defaults().Spell(glyph.StatusCheckPassing)+` golangci-lint +1m 12s`, frame)
	})

	t.Run("the steps follow the cursor", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, listed)
		harness.Press(append(everyStatusCheck, "k", "j")...)

		frame := harness.Frame()
		assert.NotContains(t, frame, "golangci-lint")
		assert.Regexp(t, glyph.Defaults().Spell(glyph.StatusCheckPassing)+` Checkout +5s`, frame)
		assert.Regexp(t, glyph.Defaults().Spell(glyph.StatusCheckFailing)+` Run tests +3m 08s`, frame)
	})

	t.Run("a status check with no steps says so", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, listed)
		harness.Press(append(everyStatusCheck, "j")...)

		frame := harness.Frame()
		assert.Contains(t, frame, "no steps reported")
		assert.NotContains(t, frame, "0 steps")
	})

	t.Run("a long step name gives way to its duration in a narrow pane", func(t *testing.T) {
		t.Parallel()

		long := newStatusCheck("1", "integration", done, "SUCCESS")
		long.Steps = []*pullrequestv1.StatusCheckStep{
			newStatusCheckStep("Run integration tests against Postgres and the forge", done, "success", time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), time.Date(2026, 10, 4, 12, 3, 8, 0, time.UTC)),
		}

		harness := openStatusChecks(t, answerStatusChecks(long))
		harness.Send(tea.WindowSizeMsg{Width: 80, Height: 40})

		assert.Regexp(t, glyph.Defaults().Spell(glyph.StatusCheckPassing)+` Run integration[^│]*… +3m 08s`, harness.Frame())
	})

	t.Run("a status check komrad stopped reading in flight says unknown without a clock", func(t *testing.T) {
		t.Parallel()

		stale := newStatusCheck("1", "e2e", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_UNKNOWN, "")
		stale.StartedAt = timestamppb.New(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC))

		harness := openStatusChecks(t, answerStatusChecks(stale))

		frame := harness.Frame()
		assert.Contains(t, frame, glyph.Defaults().Spell(glyph.StatusCheckUnknown)+" e2e")
		assert.Contains(t, frame, "unknown")
		assert.False(t, harness.IsSpinning())
	})
}

// TestStatusChecksNewestHead covers that a pull request's older heads never reach the screen, through pushes and before a new head is read.
func TestStatusChecksNewestHead(t *testing.T) {
	t.Parallel()

	done := pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED
	first := &pullrequestv1.StatusCheckReading{
		HeadSha: "1111111111111111",
		StatusChecks: []*pullrequestv1.StatusCheck{
			inStatusCheckRun(newStatusCheck("1", "lint", done, "FAILURE"), "ci", time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)),
			inStatusCheckRun(newStatusCheck("2", "test", done, "SUCCESS"), "ci", time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)),
			inStatusCheckRun(newStatusCheck("9", "test", done, "SUCCESS"), "Scanner", time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)),
		},
	}
	second := &pullrequestv1.StatusCheckReading{
		HeadSha: "2222222222222222",
		StatusChecks: []*pullrequestv1.StatusCheck{
			inStatusCheckRun(newStatusCheck("4", "vet", done, "FAILURE"), "ci", time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)),
			inStatusCheckRun(newStatusCheck("5", "test", done, "SUCCESS"), "ci", time.Date(2026, 9, 20, 12, 30, 0, 0, time.UTC)),
			inStatusCheckRun(newStatusCheck("6", "test", done, "SUCCESS"), "Scanner", time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)),
			newStatusCheck("7", "ci/legacy", done, "SUCCESS"),
		},
	}
	listed := answerStatusCheckReadings(first, second)

	tests := []struct {
		name  string
		press []string
		want  []string
	}{
		{
			name:  "every status check on the newest commit is listed",
			press: everyStatusCheck,
			want:  []string{"vet", "test", "test", "ci/legacy"},
		},
		{
			name:  "failing lists the failures of the newest commit",
			press: []string{"F", "tab", "tab", "esc"},
			want:  []string{"vet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openStatusChecksOn(t, "2222222222222222", listed)
			harness.Press(tt.press...)

			frame := harness.Frame()
			assert.Equal(t, tt.want, paneltest.ListRows(frame))
			assert.Contains(t, frame, "222222222222")
			assert.NotContains(t, frame, "111111111111")
		})
	}

	moveHead := func(harness *paneltest.Panel, head string) {
		harness.Watch(paneltest.Puts(newPullRequest(head)))
	}

	pushed := func(harness *paneltest.Panel) {
		answerStatusCheckReadings(first, second, &pullrequestv1.StatusCheckReading{
			HeadSha:      "3333333333333333",
			StatusChecks: []*pullrequestv1.StatusCheck{inStatusCheckRun(newStatusCheck("8", "build", done, "SUCCESS"), "ci", time.Date(2026, 9, 20, 13, 0, 0, 0, time.UTC))},
		})(harness)
		moveHead(harness, "3333333333333333")
	}

	t.Run("the newest commit follows a push", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecksOn(t, "2222222222222222", listed)
		pushed(harness)

		frame := harness.Frame()
		assert.Equal(t, []string{"build"}, paneltest.ListRows(frame))
		assert.Contains(t, frame, "333333333333")
	})

	t.Run("a picked scope stays through a push that passes every status check", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecksOn(t, "2222222222222222", listed)
		harness.Press("F", "tab", "tab", "esc")
		pushed(harness)

		assert.Contains(t, harness.Frame(), "no failing status checks")
	})

	t.Run("a push whose head is not read yet shows no status checks until it is", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecksOn(t, "2222222222222222", listed)
		moveHead(harness, "3333333333333333")

		frame := harness.Frame()
		assert.Contains(t, frame, "no status checks yet")
		assert.NotContains(t, frame, "222222222222")

		pushed(harness)

		assert.Equal(t, []string{"build"}, paneltest.ListRows(harness.Frame()))
	})
}

var everyStatusCheck = []string{"F", "shift+tab", "esc"}

func openStatusChecks(t *testing.T, answer func(*paneltest.Panel)) *paneltest.Panel {
	t.Helper()

	return openStatusChecksOn(t, "c1", answer)
}

func openStatusChecksOn(t *testing.T, head string, answer func(*paneltest.Panel)) *paneltest.Panel {
	t.Helper()

	agent := &agentv1.Agent{
		Id:         "n1",
		Name:       "worker",
		WorkflowId: "w1",
		Cwd:        "/repo",
		State:      agentv1.AgentState_AGENT_STATE_WAITING,
	}
	task := paneltest.TaskOf(agent)
	task.RepositoryUrl = xptr.Ptr(repositoryURL)
	task.Branch = xptr.Ptr("agent/n1")

	harness := paneltest.Start(t, paneltest.Params{})
	harness.AnswerWorktreeReads()
	harness.Watch(paneltest.Snapshot(paneltest.Workflow("w1", "backend"), agent, &pullrequestv1.Repository{Url: repositoryURL}, newPullRequest(head), task))
	harness.Press("g", "g", "j")
	answer(harness)
	harness.Press("space", "x")

	return harness
}

const repositoryURL = "https://github.com/harrisoncramer/komrad"

func newPullRequest(head string) *pullrequestv1.PullRequest {
	return &pullrequestv1.PullRequest{
		Id:                "pr-1",
		RepositoryUrl:     repositoryURL,
		HeadRepositoryUrl: repositoryURL,
		HeadBranch:        "agent/n1",
		Number:            7,
		State:             pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		OpenedAt:          timestamppb.New(time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)),
		HeadSha:           head,
	}
}

func answerStatusChecks(checks ...*pullrequestv1.StatusCheck) func(*paneltest.Panel) {
	return answerStatusCheckReadings(&pullrequestv1.StatusCheckReading{HeadSha: "c1", StatusChecks: checks})
}

func answerStatusCheckReadings(readings ...*pullrequestv1.StatusCheckReading) func(*paneltest.Panel) {
	return func(harness *paneltest.Panel) {
		harness.Answer(pullrequestv1connect.PullRequestServiceListStatusChecksProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&pullrequestv1.ListStatusChecksResponse{Readings: readings}), nil
		})
	}
}

func newStatusCheck(id, name string, state pullrequestv1.StatusCheckState, conclusion string) *pullrequestv1.StatusCheck {
	return &pullrequestv1.StatusCheck{
		ProviderId: id,
		Name:       name,
		State:      state,
		Conclusion: conclusion,
		Passed:     fixturePassed(state, conclusion),
	}
}

func newStatusCheckStep(name string, state pullrequestv1.StatusCheckState, conclusion string, startedAt, finishedAt time.Time) *pullrequestv1.StatusCheckStep {
	return &pullrequestv1.StatusCheckStep{
		Name:       name,
		State:      state,
		Conclusion: conclusion,
		Passed:     fixturePassed(state, conclusion),
		StartedAt:  dates.Stamp(startedAt),
		FinishedAt: dates.Stamp(finishedAt),
	}
}

func fixturePassed(state pullrequestv1.StatusCheckState, conclusion string) bool {
	return state == pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED && forge.IsPassingConclusion(strings.ToLower(conclusion))
}

func inStatusCheckRun(check *pullrequestv1.StatusCheck, source string, startedAt time.Time) *pullrequestv1.StatusCheck {
	check.Source = source
	check.RunStartedAt = dates.Stamp(startedAt)

	return check
}
