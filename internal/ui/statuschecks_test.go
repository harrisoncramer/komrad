package ui_test

import (
	"testing"

	"komrad/internal/glyph"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	"komrad/proto/gen/pullrequest/v1/pullrequestv1connect"

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
	return &pullrequestv1.StatusCheck{ProviderId: id, Name: name, State: state, Conclusion: conclusion}
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
		assert.Contains(t, frame, "success "+glyph.Defaults().Spell(glyph.StatusCheckPassing)+" │ lint")
		assert.Contains(t, frame, "skipped "+glyph.Defaults().Spell(glyph.StatusCheckSkipped)+" │ docs")
		assert.Contains(t, frame, "failure "+glyph.Defaults().Spell(glyph.StatusCheckFailing)+" │ vet")
		assert.Contains(t, frame, "running ⠋ │ test")
		assert.Contains(t, frame, "neutral "+glyph.Defaults().Spell(glyph.StatusCheckSkipped)+" │ deploy")
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
