package ui

import (
	"context"
	"errors"
	"testing"
	"time"

	agentv1 "weave/proto/gen/agent/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refusingHolds refuses the first release and takes every one after it.
type refusingHolds struct {
	recordingHolds
	refusals int
}

func (r *refusingHolds) ReleaseAgent(ctx context.Context, req *connect.Request[agentv1.ReleaseAgentRequest]) (*connect.Response[agentv1.ReleaseAgentResponse], error) {
	if r.refusals > 0 {
		r.refusals--

		return nil, errors.New("the input box would not take the words")
	}

	return r.recordingHolds.ReleaseAgent(ctx, req)
}

// TestARefusedUnpauseKeepsTheEditedFiles covers the retry the panel's own advice asks for.
func TestARefusedUnpauseKeepsTheEditedFiles(t *testing.T) {
	at := time.Now()
	agents := &refusingHolds{refusals: 1}

	m := heldModel(agents, &at)
	m.rememberEdited("a1", []string{"internal/agents/hold.go"})

	refused, cmd := m.Update(keyMsg("p"))
	first := asModel(t, refused)
	first = settle(t, first, cmd)

	require.Empty(t, agents.released, "the refusal sent nothing")

	pressed, cmd := first.Update(keyMsg("p"))
	retried := asModel(t, pressed)
	settle(t, retried, cmd)

	require.Len(t, agents.released, 1)
	assert.Contains(t, agents.released[0].GetSaid(), "internal/agents/hold.go",
		"the retry after a refusal still names the edited file")
}

// TestASilentUnpauseForgetsTheEditedFiles covers the release that says nothing.
func TestASilentUnpauseForgetsTheEditedFiles(t *testing.T) {
	at := time.Now()
	agents := &recordingHolds{}

	m := heldModel(agents, &at)
	m.rememberEdited("a1", []string{"internal/agents/hold.go"})

	pressed, _ := m.Update(keyMsg("P"))
	opened := asModel(t, pressed)
	require.Equal(t, formAgentRelease, opened.form.kind)

	got := answerPrompt(t, opened, "y")

	require.Len(t, agents.released, 1)
	assert.Empty(t, agents.released[0].GetSaid())
	assert.Empty(t, got.edited["a1"], "a silent release leaves no paths behind for a later hold")
	require.NotEmpty(t, got.messages.items)
	assert.Contains(t, got.messages.items[0].detail, "internal/agents/hold.go",
		"the person is told what the agent was not")
}

// TestALandedUnpauseForgetsTheEditedFiles covers the release that named the files, which one
// agent hears about once.
func TestALandedUnpauseForgetsTheEditedFiles(t *testing.T) {
	at := time.Now()
	agents := &recordingHolds{}

	m := heldModel(agents, &at)
	m.rememberEdited("a1", []string{"internal/agents/hold.go"})

	pressed, cmd := m.Update(keyMsg("p"))
	got := settle(t, asModel(t, pressed), cmd)

	require.Len(t, agents.released, 1)
	assert.Contains(t, agents.released[0].GetSaid(), "internal/agents/hold.go")
	assert.Empty(t, got.edited["a1"], "a release that named the files forgets them")
}

// TestALandedUnpauseKeepsAFileOpenedWhileItFlew covers a path remembered after the release was
// built.
func TestALandedUnpauseKeepsAFileOpenedWhileItFlew(t *testing.T) {
	at := time.Now()
	agents := &recordingHolds{}

	m := heldModel(agents, &at)
	m.rememberEdited("a1", []string{"internal/agents/hold.go"})

	pressed, cmd := m.Update(keyMsg("p"))
	sent := asModel(t, pressed)
	sent.rememberEdited("a1", []string{"internal/ui/refresh.go"})

	got := settle(t, sent, cmd)

	require.Len(t, agents.released, 1)
	assert.NotContains(t, agents.released[0].GetSaid(), "internal/ui/refresh.go")
	assert.Equal(t, []string{"internal/ui/refresh.go"}, got.edited["a1"],
		"a landing forgets the files it named and leaves the rest")
}

// TestARefusedEndOfAReviewPassKeepsTheEditedFiles covers the release the review screen sends at
// the end of a pass.
func TestARefusedEndOfAReviewPassKeepsTheEditedFiles(t *testing.T) {
	at := time.Now()
	agents := &refusingHolds{refusals: 1}

	m := heldModel(agents, &at)
	m.review.agentID = "a1"
	m.review.agentName = "telescope"
	m.review.pass.ourPause = true
	m.review.pass.touched = []string{"internal/agents/hold.go"}

	ended, cmd := m.endReviewPass("read your worktree.")
	got := settle(t, asModel(t, ended), cmd)

	require.Empty(t, agents.released, "the refusal sent nothing")
	assert.Contains(t, got.edited["a1"], "internal/agents/hold.go",
		"a refused release leaves the reader's files for the next press")
}
