package fail_test

import (
	"errors"
	"fmt"
	"testing"

	"komrad/internal/fail"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSentinelCarriesItsKind covers the kind a sentinel was declared with travelling with it
// through a wrapping.
func TestSentinelCarriesItsKind(t *testing.T) {
	sentinel := fail.NotFound("agent not found")

	wrapped := fmt.Errorf("failed to read the agent: %w", sentinel)

	require.ErrorIs(t, wrapped, sentinel)

	found := fail.Of(wrapped)
	require.NotNil(t, found)
	assert.Equal(t, fail.KindNotFound, found.Kind())
	assert.Equal(t, "agent not found", found.Error(), "a sentinel nobody worded keeps its own message")
}

// TestSaying covers a sentinel worded for one request, which still compares as itself.
func TestSaying(t *testing.T) {
	sentinel := fail.NotFound("agent not found")

	said := sentinel.Saying("no agent with id %q", "a-1")

	require.ErrorIs(t, said, sentinel)
	assert.Equal(t, `no agent with id "a-1"`, said.Error(), "the sentinel's own wording is replaced")

	found := fail.Of(said)
	require.NotNil(t, found)
	assert.Equal(t, fail.KindNotFound, found.Kind())
	assert.Equal(t, `no agent with id "a-1"`, found.Error())
}

// TestSayingSurvivesWrapping covers the domain putting its own prefix on a worded sentinel on the
// way up.
func TestSayingSurvivesWrapping(t *testing.T) {
	sentinel := fail.NotFound("schedule not found")

	wrapped := fmt.Errorf("failed to get schedule %q: %w", "s-1", sentinel.Saying("no schedule with id %q", "s-1"))

	found := fail.Of(wrapped)
	require.NotNil(t, found)
	assert.Equal(t, `no schedule with id "s-1"`, found.Error(), "the prefix the domain added is not what the caller is told")
}

// TestOfPlainError covers an error holding no sentinel at all.
func TestOfPlainError(t *testing.T) {
	assert.Nil(t, fail.Of(errors.New("connection refused")))
	assert.Nil(t, fail.Of(nil))
}

// TestOfFindsTheOutermost covers an error wrapping two sentinels, where the one it was
// wrapped in first is the one a caller is answered with.
func TestOfFindsTheOutermost(t *testing.T) {
	outer := fail.Precondition("the provisioner failed")
	inner := fail.InvalidArgument("unusable working directory")

	found := fail.Of(fmt.Errorf("%w: %w", outer, inner))

	require.NotNil(t, found)
	assert.Equal(t, fail.KindPrecondition, found.Kind())
}
