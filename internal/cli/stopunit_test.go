package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequireNotUnitManaged covers a stop typed on a host where systemd runs the service.
func TestRequireNotUnitManaged(t *testing.T) {
	t.Run("a host with the unit installed refuses", func(t *testing.T) {
		unit := filepath.Join(t.TempDir(), "weave.service")
		require.NoError(t, os.WriteFile(unit, []byte("[Unit]\n"), 0o644))

		err := requireNotUnitManaged(unit)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "systemctl restart weave")
		assert.NotContains(t, err.Error(), "mise")
	})

	t.Run("a host without it stops as before", func(t *testing.T) {
		assert.NoError(t, requireNotUnitManaged(filepath.Join(t.TempDir(), "weave.service")))
	})
}
