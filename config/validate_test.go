package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadTags covers the tags list, where a glyph must carry nothing a terminal would read
// as an instruction and a colour must be one lipgloss knows.
func TestLoadTags(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		want    []Tag
		wantErr bool
	}{
		{
			name:    "a glyph carrying a tab, which would break the row it is drawn on",
			file:    "tags = [{ glyph = \"a\\tb\" }]\n",
			wantErr: true,
		},
		{
			name:    "an empty glyph, which would draw as nothing at all",
			file:    "tags = [{ glyph = \"\" }]\n",
			wantErr: true,
		},
		{
			name:    "the same glyph twice, which would sit twice in the cycle",
			file:    "tags = [{ glyph = \"●\" }, { glyph = \"●\" }]\n",
			wantErr: true,
		},
		{
			name:    "a colour that is neither a name nor hex",
			file:    "tags = [{ glyph = \"●\", color = \"hot pink\" }]\n",
			wantErr: true,
		},
		{
			name:    "a colour missing its hash",
			file:    "tags = [{ glyph = \"●\", color = \"F932A1\" }]\n",
			wantErr: true,
		},
		{
			name:    "a colour named as an index rather than by name, which is the panel's own spelling and not a config's",
			file:    "tags = [{ glyph = \"●\", color = \"4\" }]\n",
			wantErr: true,
		},
		{
			name: "a file that names none falls back to the defaults",
			file: "default_harness = \"claude\"\n",
			want: DefaultTags(),
		},
		{
			name: "the glyphs the user chose, in their order, with their colours",
			file: "tags = [{ glyph = \"◐\", color = \"#F932A1\" }, { glyph = \"✓\", color = \"#0f0\" }]\n",
			want: []Tag{
				{Glyph: "◐", Color: "#F932A1"},
				{Glyph: "✓", Color: "#0f0"},
			},
		},
		{
			name: "colours named from the sixteen, which follow the terminal's own scheme",
			file: "tags = [{ glyph = \"◐\", color = \"blue\" }, { glyph = \"✓\", color = \"brightmagenta\" }]\n",
			want: []Tag{
				{Glyph: "◐", Color: "blue"},
				{Glyph: "✓", Color: "brightmagenta"},
			},
		},
		{
			name: "a glyph with no colour of its own, which the panel draws in the default",
			file: "[[tags]]\nglyph = \"⚑\"\n",
			want: []Tag{{Glyph: "⚑"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			cfg, err := Load([]string{path})
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Tags)
		})
	}
}

// TestLoadLayout covers the sizes and sides the panel lays its panes out at, since tmux is
// given either a share of what it divides or a whole number of cells.
func TestLoadLayout(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		want    Layout
		wantErr bool
	}{
		{
			name:    "a negative width, which is not a size at all",
			file:    "[layout]\nsidebar_width = -0.2\n",
			wantErr: true,
		},
		{
			name:    "a width between a share and a column count",
			file:    "[layout]\nsidebar_width = 1.5\n",
			wantErr: true,
		},
		{
			name:    "a height between a share and a row count",
			file:    "[layout]\ninfo_height = 2.5\n",
			wantErr: true,
		},
		{
			name:    "a side the list cannot sit on",
			file:    "[layout]\nsidebar_side = \"top\"\n",
			wantErr: true,
		},
		{
			name:    "a side the info pane cannot open on",
			file:    "[layout]\ninfo_side = \"right\"\n",
			wantErr: true,
		},
		{
			name: "a file that names no layout falls back to the default",
			file: "default_harness = \"claude\"\n",
			want: DefaultLayout(),
		},
		{
			name: "the share the user chose, with the rest left at the defaults",
			file: "[layout]\nsidebar_width = 0.45\n",
			want: Layout{
				SidebarSide:   DefaultSidebarSide,
				SidebarWidth:  0.45,
				InfoSide:      DefaultInfoSide,
				InfoHeight:    DefaultInfoHeight,
				DetailsHeight: DefaultDetailsHeight,
			},
		},
		{
			name: "a width in columns rather than a share of the popup",
			file: "[layout]\nsidebar_width = 40\n",
			want: Layout{
				SidebarSide:   DefaultSidebarSide,
				SidebarWidth:  40,
				InfoSide:      DefaultInfoSide,
				InfoHeight:    DefaultInfoHeight,
				DetailsHeight: DefaultDetailsHeight,
			},
		},
		{
			name: "the sides the user chose for the list and the info pane",
			file: "[layout]\nsidebar_side = \"right\"\ninfo_side = \"above\"\n",
			want: Layout{
				SidebarSide:   SidebarSideRight,
				SidebarWidth:  DefaultSidebarWidth,
				InfoSide:      InfoSideAbove,
				InfoHeight:    DefaultInfoHeight,
				DetailsHeight: DefaultDetailsHeight,
			},
		},
		{
			name: "the height the user chose for the info pane",
			file: "[layout]\ninfo_height = 0.6\n",
			want: Layout{
				SidebarSide:   DefaultSidebarSide,
				SidebarWidth:  DefaultSidebarWidth,
				InfoSide:      DefaultInfoSide,
				InfoHeight:    0.6,
				DetailsHeight: DefaultDetailsHeight,
			},
		},
		{
			name: "the height the user chose for the details under the list",
			file: "[layout]\ndetails_height = 12\n",
			want: Layout{
				SidebarSide:   DefaultSidebarSide,
				SidebarWidth:  DefaultSidebarWidth,
				InfoSide:      DefaultInfoSide,
				InfoHeight:    DefaultInfoHeight,
				DetailsHeight: 12,
			},
		},
		{
			name:    "a details height between a share and a row count",
			file:    "[layout]\ndetails_height = 2.5\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			cfg, err := Load([]string{path})
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Layout)
		})
	}
}

// TestLoadMaxAgents covers the ceiling on how many agents may exist, which an omitted key
// takes the built-in for rather than reading as none.
func TestLoadMaxAgents(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		want    int
		wantErr bool
	}{
		{
			name: "a file naming none takes the built-in",
			file: "default_harness = \"claude\"\n",
			want: DefaultMaxAgents,
		},
		{
			name: "the ceiling the user chose",
			file: "max_agents = 12\n",
			want: 12,
		},
		{
			// Zero reads the same as the key being absent, so it cannot mean no ceiling.
			name: "zero is an omitted key rather than a ceiling of none",
			file: "max_agents = 0\n",
			want: DefaultMaxAgents,
		},
		{
			name:    "a negative ceiling is not a number of agents",
			file:    "max_agents = -1\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			cfg, err := Load([]string{path})
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.MaxAgents)
		})
	}
}

// TestLoadLogLevel covers the level the service logs at, since a word slog has no answer for
// would decode as debug and turn the log all the way up.
func TestLoadLogLevel(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		want    string
		wantErr bool
	}{
		{
			name: "a file that says nothing about logging",
			want: DefaultLogLevel,
		},
		{
			name: "a level spelled as slog spells it",
			file: "[log]\nlevel = \"debug\"\n",
			want: "debug",
		},
		{
			name: "a level in capitals, since a config is written by hand",
			file: "[log]\nlevel = \"ERROR\"\n",
			want: "ERROR",
		},
		{
			name:    "a level that is not one",
			file:    "[log]\nlevel = \"verbose\"\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			cfg, err := Load([]string{path})
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Log.Level)
		})
	}
}
