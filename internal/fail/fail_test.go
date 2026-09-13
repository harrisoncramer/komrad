package fail_test

import (
	"errors"
	"fmt"
	"testing"

	"weave/internal/fail"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSentinelCarriesItsKind covers the kind a sentinel was declared with travelling with it
// through a wrapping.
func TestSentinelCarriesItsKind(t *testing.T) {
	sentinel := fail.NotFound("agent not found")

	wrapped := fmt.Errorf("failed to read the agent: %w", sentinel)

	require.ErrorIs(t, wrapped, sentinel)

	found, wording := fail.Answer(wrapped)
	require.NotNil(t, found)
	assert.Equal(t, fail.KindNotFound, found.Kind())
	assert.Empty(t, wording, "a sentinel nobody worded answers with its own message")
}

// TestSaying covers a sentinel worded for one request, which still compares as itself.
func TestSaying(t *testing.T) {
	sentinel := fail.NotFound("agent not found")

	said := sentinel.Saying("no agent with id %q", "a-1")

	require.ErrorIs(t, said, sentinel)
	assert.Equal(t, `no agent with id "a-1"`, said.Error(), "the sentinel's own wording is replaced")

	found, wording := fail.Answer(said)
	require.NotNil(t, found)
	assert.Equal(t, fail.KindNotFound, found.Kind())
	assert.Equal(t, `no agent with id "a-1"`, wording)
}

// TestSayingSurvivesWrapping covers the domain putting its own prefix on a worded sentinel on
// the way up, which is for the log rather than for whoever asked.
func TestSayingSurvivesWrapping(t *testing.T) {
	sentinel := fail.NotFound("schedule not found")

	wrapped := fmt.Errorf("failed to get schedule %q: %w", "s-1", sentinel.Saying("no schedule with id %q", "s-1"))

	found, wording := fail.Answer(wrapped)
	require.NotNil(t, found)
	assert.Equal(t, `no schedule with id "s-1"`, wording, "the prefix the domain added is not what the caller is told")
}

// TestAnswerPlainError covers an error holding no sentinel at all.
func TestAnswerPlainError(t *testing.T) {
	found, wording := fail.Answer(errors.New("connection refused"))
	assert.Nil(t, found)
	assert.Empty(t, wording)

	found, _ = fail.Answer(nil)
	assert.Nil(t, found)
}

// TestAnswerFindsTheOutermost covers an error wrapping two sentinels, where the one it was
// wrapped in first is the one a caller is answered with.
func TestAnswerFindsTheOutermost(t *testing.T) {
	outer := fail.Precondition("the provisioner failed")
	inner := fail.InvalidArgument("unusable working directory")

	found, _ := fail.Answer(fmt.Errorf("%w: %w", outer, inner))

	require.NotNil(t, found)
	assert.Equal(t, fail.KindPrecondition, found.Kind())
}
