package agent

import (
	"testing"

	"weave/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListByIDIgnoresAnIDNoRowCouldHold pins the guard it opens with, which a string that is not an id at all would otherwise reach Postgres as a type error past.
func TestListByIDIgnoresAnIDNoRowCouldHold(t *testing.T) {
	got, err := open(t).ListByID(t.Context(), []string{"not-a-uuid", ""})

	require.NoError(t, err, "an id no row could hold is nothing to do rather than something to report")
	assert.Empty(t, got, "no row answers to either of those")
}

// TestGetInWorkflowAnswersNotFoundForANameNobodyHolds pins a reader that speaks for one agent answering not found rather than nothing.
func TestGetInWorkflowAnswersNotFoundForANameNobodyHolds(t *testing.T) {
	st := open(t)

	_, err := st.GetInWorkflow(t.Context(), st.defaultWorkflow, "nobody")

	require.ErrorIs(t, err, store.ErrNotFound)
}
