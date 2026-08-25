package server

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"weave/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMailConfigReadsWhatTheFileSays covers the ordinary case, so the refusal below cannot be
// satisfied by a wrapper that refuses everything.
func TestMailConfigReadsWhatTheFileSays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[mail]\ndelivery_deadline = \"45m\"\nmax_hops = 7\n"), 0o600))

	load := mailConfig(func() (*config.Config, error) { return config.Load([]string{path}) })

	deadline, err := load.GetDeliveryDeadline()
	require.NoError(t, err)
	assert.Equal(t, "45m0s", deadline.String())

	hops, err := load.GetMaxHops()
	require.NoError(t, err)
	assert.Equal(t, 7, hops)
}

// TestMailConfigRefusesAConfigItCannotRead covers a typo in config.toml. The config is read
// fresh on every send, so a file that stops loading under a running server must refuse the
// send rather than swap in a deadline nobody wrote.
func TestMailConfigRefusesAConfigItCannotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[mail]\ndelivery_dedline = \"45m\"\n"), 0o600))

	load := mailConfig(func() (*config.Config, error) { return config.Load([]string{path}) })

	_, err := load.GetDeliveryDeadline()
	require.Error(t, err, "an unreadable config is an error rather than the built-in deadline")
	assert.Contains(t, err.Error(), "delivery_dedline")

	_, err = load.GetMaxHops()
	require.Error(t, err, "an unreadable config is an error rather than the built-in hop limit")
	assert.Contains(t, err.Error(), "delivery_dedline")
}

// TestMailConfigPropagatesTheLoadError covers the error the caller is handed, since a send
// refused for a reason it cannot name is not much better than one silently defaulted.
func TestMailConfigPropagatesTheLoadError(t *testing.T) {
	sentinel := errors.New("no config here")
	load := mailConfig(func() (*config.Config, error) { return nil, sentinel })

	_, err := load.GetDeliveryDeadline()
	assert.ErrorIs(t, err, sentinel)

	_, err = load.GetMaxHops()
	assert.ErrorIs(t, err, sentinel)
}
