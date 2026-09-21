package scripts

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// write puts an empty script at the path.
func write(t *testing.T, path string, mode os.FileMode) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), mode))
}
