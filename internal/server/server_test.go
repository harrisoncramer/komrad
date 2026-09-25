package server

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"komrad/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMailConfigReadsWhatTheFileSays covers the ordinary case, so the refusal below cannot be
// satisfied by a wrapper that refuses everything.
func TestMailConfigReadsWhatTheFileSays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[mail]\ndelivery_deadline = \"45m\"\nmax_exchange = 7\n"), 0o600))

	load := mailConfig(func() (*config.Config, error) { return config.LoadStack(config.Stack{Paths: []string{path}}) })

	deadline, err := load.GetDeliveryDeadline()
	require.NoError(t, err)
	assert.Equal(t, 45*time.Minute, deadline)

	exchange, err := load.GetMaxExchange()
	require.NoError(t, err)
	assert.Equal(t, 7, exchange)
}

// TestMailConfigRefusesAConfigItCannotRead covers a typo appearing in config.toml under a
// running server, which must refuse the send rather than swap in a deadline nobody wrote.
func TestMailConfigRefusesAConfigItCannotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[mail]\ndelivery_dedline = \"45m\"\n"), 0o600))

	load := mailConfig(func() (*config.Config, error) { return config.LoadStack(config.Stack{Paths: []string{path}}) })

	_, err := load.GetDeliveryDeadline()
	require.Error(t, err, "an unreadable config is an error rather than the built-in deadline")
	assert.Contains(t, err.Error(), "delivery_dedline")

	_, err = load.GetMaxExchange()
	require.Error(t, err, "an unreadable config is an error rather than the built-in exchange limit")
	assert.Contains(t, err.Error(), "delivery_dedline")
}

// TestMailConfigPropagatesTheLoadError covers the error the caller is handed.
func TestMailConfigPropagatesTheLoadError(t *testing.T) {
	sentinel := errors.New("no config here")
	load := mailConfig(func() (*config.Config, error) { return nil, sentinel })

	_, err := load.GetDeliveryDeadline()
	require.ErrorIs(t, err, sentinel)

	_, err = load.GetMaxExchange()
	require.ErrorIs(t, err, sentinel)
}
