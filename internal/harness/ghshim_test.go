package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteGHShimNeedsSomewhereToWriteAndSomethingToRun covers a launch with no cache dir or no
// weave binary, which gets no stand-in rather than a broken one.
func TestWriteGHShimNeedsSomewhereToWriteAndSomethingToRun(t *testing.T) {
	dir, err := WriteGHShim("", "/usr/local/bin/weave")
	require.NoError(t, err)
	assert.Empty(t, dir)

	dir, err = WriteGHShim(t.TempDir(), "")
	require.NoError(t, err)
	assert.Empty(t, dir)
}
