package server

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func settingsFor(t *testing.T, body string) *frameSettings {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return newFrameSettings(func() (*config.Config, error) { return config.LoadStack(config.Stack{Paths: []string{path}}) })
}

// TestFrameSettingsAreOffUntilAskedFor covers the ordinary install, where nobody is chasing
// anything and no frame is worth the disk.
func TestFrameSettingsAreOffUntilAskedFor(t *testing.T) {
	held := settingsFor(t, "").get()

	assert.False(t, held.On)
	assert.Equal(t, config.DefaultFrameHistory, held.Keep)
}

// TestFrameSettingsReadWhatTheFileSays covers the setting actually reaching the recorder.
func TestFrameSettingsReadWhatTheFileSays(t *testing.T) {
	held := settingsFor(t, "[debug]\nframes = true\nframe_history = 20\n").get()

	assert.True(t, held.On)
	assert.Equal(t, 20, held.Keep)
}

// TestFrameSettingsAreHeldBetweenReads keeps the reconcile pass from parsing the config once per
// agent, which it asks for these on every reading.
func TestFrameSettingsAreHeldBetweenReads(t *testing.T) {
	var loads int
	settings := newFrameSettings(func() (*config.Config, error) {
		loads++

		return config.Defaults(), nil
	})

	settings.get()
	settings.get()

	assert.Equal(t, 1, loads)
}

// TestFrameSettingsReReadOnceTheyAreOld is what lets a recording be turned on under a service
// already running, which is the whole reason the config is read rather than taken at startup.
func TestFrameSettingsReReadOnceTheyAreOld(t *testing.T) {
	var loads int
	settings := newFrameSettings(func() (*config.Config, error) {
		loads++

		return config.Defaults(), nil
	})

	now := time.Now()
	settings.now = func() time.Time { return now }

	settings.get()
	now = now.Add(frameSettingsTTL + time.Second)
	settings.get()

	assert.Equal(t, 2, loads)
}

// TestFrameSettingsBackOffAfterAFailedLoad covers a typo sitting in config.toml, which is
// otherwise re-parsed once per agent on every reconcile pass for as long as it is there.
func TestFrameSettingsBackOffAfterAFailedLoad(t *testing.T) {
	var loads int
	settings := newFrameSettings(func() (*config.Config, error) {
		loads++

		return nil, errors.New("no config here")
	})

	now := time.Now()
	settings.now = func() time.Time { return now }

	settings.get()
	settings.get()
	settings.get()

	assert.Equal(t, 1, loads)
}

// TestFrameSettingsKeepWhatTheyHadWhenTheConfigBreaks stops a typo appearing in config.toml
// from turning a recording somebody is relying on off mid-chase.
func TestFrameSettingsKeepWhatTheyHadWhenTheConfigBreaks(t *testing.T) {
	broken := errors.New("no config here")

	var fail bool
	settings := newFrameSettings(func() (*config.Config, error) {
		if fail {
			return nil, broken
		}

		return &config.Config{Debug: config.Debug{Frames: true, FrameHistory: 20}}, nil
	})

	now := time.Now()
	settings.now = func() time.Time { return now }

	require.True(t, settings.get().On)

	fail = true
	now = now.Add(frameSettingsTTL + time.Second)

	assert.True(t, settings.get().On, "a config that will not load leaves the last settings standing")
}
