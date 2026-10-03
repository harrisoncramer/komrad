package triggers

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSyncVersionsEachUnitByItsSQL covers a fresh directory, a unit whose SQL changed, a unit left alone and a unit no longer declared.
func TestSyncVersionsEachUnitByItsSQL(t *testing.T) {
	units, err := Render()
	require.NoError(t, err)
	require.Greater(t, len(units), 2)
	changed, kept := units[1], units[2]

	dir := t.TempDir()
	require.NoError(t, Sync(dir))
	assert.Equal(t, fileNames(units, map[string]int{}), listDir(t, dir))

	require.NoError(t, os.WriteFile(filepath.Join(dir, changed.Name+".v1.sql"), []byte(header+"stale\n"), 0o644))
	keptContent, err := os.ReadFile(filepath.Join(dir, kept.Name+".v1.sql"))
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(dir, kept.Name+".v1.sql")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, kept.Name+".v4.sql"), keptContent, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gone.v3.sql"), []byte(header+"stale\n"), 0o644))
	require.NoError(t, Sync(dir))

	assert.Equal(t, fileNames(units, map[string]int{changed.Name: 2, kept.Name: 4}), listDir(t, dir))
	content, err := os.ReadFile(filepath.Join(dir, changed.Name+".v2.sql"))
	require.NoError(t, err)
	assert.Equal(t, header+fingerprintOf(changed.SQL)+"\n"+changed.SQL, string(content))
}

// TestSyncRefusesADirectoryItCannotRead covers a misnamed file, two files for one unit and a file without its header.
func TestSyncRefusesADirectoryItCannotRead(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
	}{
		{
			name:  "a file not named unit.vN.sql",
			files: map[string]string{"agents.sql": header + "x\n"},
		},
		{
			name:  "two files for one unit",
			files: map[string]string{"agents.v1.sql": header + "x\n", "agents.v2.sql": header + "x\n"},
		},
		{
			name:  "a file without its header",
			files: map[string]string{"agents.v1.sql": "CREATE TRIGGER\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
			}

			assert.Error(t, Sync(dir))
		})
	}
}

func fileNames(units []Unit, versions map[string]int) []string {
	names := make([]string, len(units))
	for i, unit := range units {
		version := max(versions[unit.Name], 1)
		names[i] = fmt.Sprintf("%s.v%d.sql", unit.Name, version)
	}
	slices.Sort(names)

	return names
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}

	return names
}
