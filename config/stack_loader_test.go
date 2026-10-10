package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStackLoader covers reading a config once and again only after one of its files changes.
func TestStackLoader(t *testing.T) {
	t.Run("a broken edit is reported and its fix read", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(path, []byte("[log]\nlevel = \"warn\"\n"), 0o600))
		loader := NewStackLoader(Stack{Paths: []string{path}})
		_, err := loader.Load()
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(path, []byte("[log]\nlevel = \"loud\"\n"), 0o600))
		_, err = loader.Load()
		require.Error(t, err)

		require.NoError(t, os.WriteFile(path, []byte("[log]\nlevel = \"debug\"\n"), 0o600))
		fixed, err := loader.Load()
		require.NoError(t, err)
		assert.Equal(t, "debug", fixed.Log.Level)
	})

	t.Run("a dangling link where nothing was is read again", func(t *testing.T) {
		dir := t.TempDir()
		pending := filepath.Join(dir, "pending.toml")
		loader := NewStackLoader(Stack{Paths: []string{pending}})
		_, err := loader.Load()
		require.ErrorContains(t, err, "there is no file there")

		require.NoError(t, os.Symlink(filepath.Join(dir, "missing.toml"), pending))
		_, err = loader.Load()
		require.ErrorContains(t, err, "which does not exist")
	})

	t.Run("an unchanged stack is not read again", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(path, []byte("[log]\nlevel = \"warn\"\n"), 0o600))
		loader := NewStackLoader(Stack{Paths: []string{path}})
		first, err := loader.Load()
		require.NoError(t, err)
		assert.Equal(t, "warn", first.Log.Level)

		again, err := loader.Load()
		require.NoError(t, err)
		assert.Same(t, first, again)
	})

	t.Run("an edit is read on the next load", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(path, []byte("[log]\nlevel = \"warn\"\n"), 0o600))
		loader := NewStackLoader(Stack{Paths: []string{path}})
		_, err := loader.Load()
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(path, []byte("[log]\nlevel = \"error\"\n"), 0o600))
		edited, err := loader.Load()
		require.NoError(t, err)
		assert.Equal(t, "error", edited.Log.Level)
	})

	t.Run("a link repointed at a file of the same size and time is read again", func(t *testing.T) {
		dir := t.TempDir()
		stamp := time.Unix(1, 0)
		targets := []string{filepath.Join(dir, "a.toml"), filepath.Join(dir, "b.toml")}
		for i, level := range []string{"info", "warn"} {
			require.NoError(t, os.WriteFile(targets[i], []byte("[log]\nlevel = \""+level+"\"\n"), 0o600))
			require.NoError(t, os.Chtimes(targets[i], stamp, stamp))
		}
		link := filepath.Join(dir, "linked.toml")
		require.NoError(t, os.Symlink(targets[0], link))

		loader := NewStackLoader(Stack{Paths: []string{link}})
		before, err := loader.Load()
		require.NoError(t, err)
		assert.Equal(t, "info", before.Log.Level)

		require.NoError(t, os.Remove(link))
		require.NoError(t, os.Symlink(targets[1], link))
		after, err := loader.Load()
		require.NoError(t, err)
		assert.Equal(t, "warn", after.Log.Level)
	})
}
