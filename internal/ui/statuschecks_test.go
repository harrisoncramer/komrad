package ui_test

import (
	"testing"

	"komrad/internal/glyph"
	"komrad/lib/xptr"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	"komrad/proto/gen/pullrequest/v1/pullrequestv1connect"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
)

const statusChecksScreen = "the status checks"

func openStatusChecks(t *testing.T, answer func(*panelHarness)) *panelHarness {
	t.Helper()

	harness := openNoticed(t, panelHarnessParams{}, newPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN)...)
	answer(harness)
	harness.press("space", "x")

	return harness
}

func answerStatusChecks(checks ...*pullrequestv1.StatusCheck) func(*panelHarness) {
	return func(harness *panelHarness) {
		harness.answer(pullrequestv1connect.PullRequestServiceListStatusChecksProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&pullrequestv1.ListStatusChecksResponse{HeadSha: "c1", StatusChecks: checks}), nil
		})
	}
}

func newStatusCheck(id, name string, state pullrequestv1.StatusCheckState, conclusion string) *pullrequestv1.StatusCheck {
	return &pullrequestv1.StatusCheck{
		ProviderId: id,
		Name:       name,
		State:      state,
		Conclusion: conclusion,
	}
}

// TestStatusChecksOpen covers the screen opening from the subpages menu.
func TestStatusChecksOpen(t *testing.T) {
	t.Parallel()

	t.Run("an agent with no pull request has no status checks to list", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildSubpagesAgents()...)
		moveToAgentRow(t, harness, "author")
		harness.press("space", "x")

		frame := harness.readFrame()
		assert.Equal(t, statusChecksScreen, harness.readScreen())
		assert.Contains(t, frame, "agents › author › status checks")
		assert.Contains(t, frame, "no pull request")
	})

	t.Run("back returns to the list on the agent it opened for", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildSubpagesAgents()...)
		moveToAgentRow(t, harness, "author")
		harness.press("space", "x", "esc")

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, readHoveredRow(harness.readFrame()), "author")
	})

	t.Run("closing the menu comes back to the same agent's status checks", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, answerStatusChecks(
			newStatusCheck("1", "lint", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "SUCCESS"),
		))
		harness.press("space", "esc")

		assert.Equal(t, statusChecksScreen, harness.readScreen())
		assert.Equal(t, []string{"lint"}, readListRows(harness.readFrame()))
	})
}

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

		harness.assertSentTo(pullrequestv1connect.PullRequestServiceListStatusChecksProcedure, &pullrequestv1.ListStatusChecksRequest{PullRequestId: "pr-1"})
	})

	t.Run("each status check is a row", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, listed)

		frame := harness.readFrame()
		assert.Equal(t, []string{"lint", "test", "build"}, readListRows(frame))
	})

	t.Run("each status check is drawn with the icon the sidebar draws its pull request with", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, answerStatusChecks(
			newStatusCheck("1", "lint", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "SUCCESS"),
			newStatusCheck("2", "docs", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "SKIPPED"),
			newStatusCheck("3", "vet", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "FAILURE"),
			newStatusCheck("4", "test", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING, ""),
			newStatusCheck("5", "deploy", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "NEUTRAL"),
		))

		frame := harness.readFrame()
		assert.Contains(t, frame, glyph.Defaults().Spell(glyph.StatusCheckPassing)+" │ lint")
		assert.Contains(t, frame, glyph.Defaults().Spell(glyph.StatusCheckSkipped)+" │ docs")
		assert.Contains(t, frame, glyph.Defaults().Spell(glyph.StatusCheckFailing)+" │ vet")
		assert.Contains(t, frame, "⠋ │ test")
		assert.Contains(t, frame, glyph.Defaults().Spell(glyph.StatusCheckSkipped)+" │ deploy")
		assert.NotContains(t, frame, "success")
	})

	t.Run("the spinner turns while a status check runs", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, listed)

		assert.True(t, harness.fireLoader())
	})

	t.Run("the spinner rests once every status check has finished", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, answerStatusChecks(
			newStatusCheck("1", "lint", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "SUCCESS"),
		))

		assert.False(t, harness.fireLoader())
	})

	t.Run("the cursor moves through the status checks", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, listed)
		harness.press("j")

		assert.Contains(t, readHoveredRow(harness.readFrame()), "test")
	})

	t.Run("searching lands on the status check it found", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, listed)
		harness.press("/", "b", "u", "i", "enter")

		assert.Contains(t, readHoveredRow(harness.readFrame()), "build")
	})

	t.Run("a pull request with no status checks says so", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, answerStatusChecks())

		assert.Contains(t, harness.readFrame(), "no status checks yet")
	})

	t.Run("a refused listing says why", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, func(harness *panelHarness) {
			harness.refuse(pullrequestv1connect.PullRequestServiceListStatusChecksProcedure, connect.NewError(connect.CodeUnavailable, nil))
		})

		assert.Contains(t, harness.readFrame(), "could not list")
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
	)

	tests := []struct {
		name  string
		press []string
		want  []string
	}{
		{
			name: "the listing opens on every status check",
			want: []string{"lint", "test", "vet", "docs", "build"},
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
			want:  []string{"lint", "test", "vet", "docs", "build"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openStatusChecks(t, listed)
			harness.press(tt.press...)

			assert.Equal(t, tt.want, readListRows(harness.readFrame()))
		})
	}

	t.Run("a status check both scopes list keeps the cursor", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, listed)
		harness.press("j", "j", "j", "j", "F", "tab", "esc")

		assert.Contains(t, readHoveredRow(harness.readFrame()), "build")
	})

	t.Run("a scope that holds none of the status checks says so", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, answerStatusChecks(
			newStatusCheck("1", "lint", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "SUCCESS"),
		))
		harness.press("F", "tab", "tab", "esc")

		assert.Contains(t, harness.readFrame(), "no failing status checks")
	})
}

func newStatusCheckStep(name string, state pullrequestv1.StatusCheckState, conclusion, startedAt, finishedAt string) *pullrequestv1.StatusCheckStep {
	return &pullrequestv1.StatusCheckStep{
		Name:       name,
		State:      state,
		Conclusion: conclusion,
		StartedAt:  xptr.Ptr(startedAt),
		FinishedAt: xptr.Ptr(finishedAt),
	}
}

// TestStatusChecksSteps covers the side pane drawing the steps of the hovered status check.
func TestStatusChecksSteps(t *testing.T) {
	t.Parallel()

	done := pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED

	lint := newStatusCheck("1", "lint", done, "SUCCESS")
	lint.StartedAt, lint.FinishedAt = xptr.Ptr("2026-10-04T12:00:00Z"), xptr.Ptr("2026-10-04T12:01:12Z")
	lint.Steps = []*pullrequestv1.StatusCheckStep{
		newStatusCheckStep("golangci-lint", done, "success", "2026-10-04T12:00:00Z", "2026-10-04T12:01:12Z"),
	}

	test := newStatusCheck("2", "test", done, "FAILURE")
	test.Steps = []*pullrequestv1.StatusCheckStep{
		newStatusCheckStep("Checkout", done, "success", "2026-10-04T12:00:00Z", "2026-10-04T12:00:05Z"),
		newStatusCheckStep("Run tests", done, "failure", "2026-10-04T12:00:05Z", "2026-10-04T12:03:13Z"),
	}

	status := newStatusCheck("3", "codecov", done, "SUCCESS")

	listed := answerStatusChecks(lint, test, status)

	t.Run("the hovered status check's steps are drawn with their icons and durations", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, listed)

		frame := harness.readFrame()
		assert.Contains(t, frame, "1m 12s · 1 step")
		assert.Regexp(t, glyph.Defaults().Spell(glyph.StatusCheckPassing)+` golangci-lint +1m 12s`, frame)
	})

	t.Run("the steps follow the cursor", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, listed)
		harness.press("j")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "golangci-lint")
		assert.Regexp(t, glyph.Defaults().Spell(glyph.StatusCheckPassing)+` Checkout +5s`, frame)
		assert.Regexp(t, glyph.Defaults().Spell(glyph.StatusCheckFailing)+` Run tests +3m 08s`, frame)
	})

	t.Run("a status check with no steps says so", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, listed)
		harness.press("j", "j")

		frame := harness.readFrame()
		assert.Contains(t, frame, "no steps reported")
		assert.NotContains(t, frame, "0 steps")
	})

	t.Run("a long step name gives way to its duration in a narrow pane", func(t *testing.T) {
		t.Parallel()

		long := newStatusCheck("1", "integration", done, "SUCCESS")
		long.Steps = []*pullrequestv1.StatusCheckStep{
			newStatusCheckStep("Run integration tests against Postgres and the forge", done, "success", "2026-10-04T12:00:00Z", "2026-10-04T12:03:08Z"),
		}

		harness := openStatusChecks(t, answerStatusChecks(long))
		harness.send(tea.WindowSizeMsg{Width: 80, Height: 40})

		assert.Regexp(t, glyph.Defaults().Spell(glyph.StatusCheckPassing)+` Run integration[^│]*… +3m 08s`, harness.readFrame())
	})
}
