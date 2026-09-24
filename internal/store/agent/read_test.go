package agent

import (
	"testing"

	"komrade/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListByIDIgnoresAnIDNoRowCouldHold pins the guard that keeps a malformed id from Postgres.
func TestListByIDIgnoresAnIDNoRowCouldHold(t *testing.T) {
	got, err := open(t).ListByID(t.Context(), []string{"not-a-uuid", ""})

	require.NoError(t, err, "an id no row could hold is nothing to do rather than something to report")
	assert.Empty(t, got, "no row answers to either of those")
}

// TestGetInWorkflowAnswersNotFoundForANameNobodyHolds pins store.ErrNotFound for a miss.
func TestGetInWorkflowAnswersNotFoundForANameNobodyHolds(t *testing.T) {
	st := open(t)

	_, err := st.GetInWorkflow(t.Context(), st.defaultWorkflow, "nobody")

	require.ErrorIs(t, err, store.ErrNotFound)
}
