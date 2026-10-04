package ui_test

import (
	"testing"

	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	"komrad/proto/gen/pullrequest/v1/pullrequestv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
)

const checksScreen = "the checks"

func openChecks(t *testing.T, answer func(*panelHarness)) *panelHarness {
	t.Helper()

	harness := openNoticed(t, panelHarnessParams{}, newPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN)...)
	answer(harness)
	harness.press("space", "x")

	return harness
}

func answerChecks(checks ...*pullrequestv1.Check) func(*panelHarness) {
	return func(harness *panelHarness) {
		harness.answer(pullrequestv1connect.PullRequestServiceListChecksProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&pullrequestv1.ListChecksResponse{HeadSha: "c1", Checks: checks}), nil
		})
	}
}

func newCheck(id, name string, state pullrequestv1.CheckState, conclusion string) *pullrequestv1.Check {
	return &pullrequestv1.Check{ProviderId: id, Name: name, State: state, Conclusion: conclusion}
}

// TestChecksOpen covers the screen opening from the subpages menu.
func TestChecksOpen(t *testing.T) {
	t.Parallel()

	t.Run("an agent with no pull request has no checks to list", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildSubpagesAgents()...)
		moveToAgentRow(t, harness, "author")
		harness.press("space", "x")

		frame := harness.readFrame()
		assert.Equal(t, checksScreen, harness.readScreen())
		assert.Contains(t, frame, "agents › author › checks")
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

// TestChecksList covers the checks listed for the agent's pull request.
func TestChecksList(t *testing.T) {
	t.Parallel()

	listed := answerChecks(
		newCheck("1", "lint", pullrequestv1.CheckState_CHECK_STATE_COMPLETED, "SUCCESS"),
		newCheck("2", "test", pullrequestv1.CheckState_CHECK_STATE_RUNNING, ""),
		newCheck("3", "build", pullrequestv1.CheckState_CHECK_STATE_QUEUED, ""),
	)

	t.Run("it asks for the checks of the agent's pull request", func(t *testing.T) {
		t.Parallel()

		harness := openChecks(t, listed)

		harness.assertSentTo(pullrequestv1connect.PullRequestServiceListChecksProcedure, &pullrequestv1.ListChecksRequest{PullRequestId: "pr-1"})
	})

	t.Run("each check is a row with where it stands", func(t *testing.T) {
		t.Parallel()

		harness := openChecks(t, listed)

		frame := harness.readFrame()
		assert.Equal(t, []string{"lint", "test", "build"}, readListRows(frame))
		assert.Contains(t, frame, "success │ lint")
		assert.Contains(t, frame, "running │ test")
		assert.Contains(t, frame, "queued │ build")
	})

	t.Run("the cursor moves through the checks", func(t *testing.T) {
		t.Parallel()

		harness := openChecks(t, listed)
		harness.press("j")

		assert.Contains(t, readHoveredRow(harness.readFrame()), "test")
	})

	t.Run("searching lands on the check it found", func(t *testing.T) {
		t.Parallel()

		harness := openChecks(t, listed)
		harness.press("/", "b", "u", "i", "enter")

		assert.Contains(t, readHoveredRow(harness.readFrame()), "build")
	})

	t.Run("a pull request with no checks says so", func(t *testing.T) {
		t.Parallel()

		harness := openChecks(t, answerChecks())

		assert.Contains(t, harness.readFrame(), "no checks yet")
	})

	t.Run("a refused listing says why", func(t *testing.T) {
		t.Parallel()

		harness := openChecks(t, func(harness *panelHarness) {
			harness.refuse(pullrequestv1connect.PullRequestServiceListChecksProcedure, connect.NewError(connect.CodeUnavailable, nil))
		})

		assert.Contains(t, harness.readFrame(), "could not list")
	})
}
