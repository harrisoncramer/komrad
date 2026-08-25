package actions

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fixtureDir = "testfiles"

// TestReadHeader covers what a file is taken to declare about itself, and whether it is text
// at all. Each row names a file in testfiles, and the last check refuses a fixture no row
// names.
func TestReadHeader(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		want     Header
		wantText bool
	}{
		{
			name:     "a file with nothing in it declares nothing",
			file:     "empty",
			wantText: true,
			want:     Header{},
		},
		{
			name:     "comments that are not directives are only comments",
			file:     "no-header",
			wantText: true,
			want:     Header{},
		},
		{
			name: "a file that is not text is not scanned",
			file: "not-text",
			want: Header{},
		},
		{
			name:     "an unquoted value is not TOML and is skipped",
			file:     "not-toml",
			wantText: true,
			want: Header{
				Problems: []string{`line 2: "description = just words, unquoted" is not one TOML key and value`},
			},
		},
		{
			name:     "a key weave does not know is reported rather than ignored",
			file:     "unknown-directive",
			wantText: true,
			want: Header{
				Problems: []string{
					"line 2: descriptoin is not a directive weave knows",
					"line 3: tools is not a directive weave knows",
				},
			},
		},
		{
			name:     "a duration nothing can read leaves the timeout unset",
			file:     "malformed-duration",
			wantText: true,
			want: Header{
				Description: "Names a duration nothing can read.",
				Problems:    []string{`line 3: "soon" is not a duration: time: invalid duration "soon"`},
			},
		},
		{
			name:     "a directive below the first line of code does not count",
			file:     "below-code",
			wantText: true,
			want: Header{
				Description: "Declared in the header, where a declaration belongs.",
			},
		},
		{
			name:     "quotes and unicode survive the way TOML says they do",
			file:     "quoted-unicode",
			wantText: true,
			want: Header{
				Description: "Says \"hello\" to the ✨ agent — em dash and all",
			},
		},
		{
			name:     "directives are read from the first line down",
			file:     "well-formed",
			wantText: true,
			want: Header{
				Description: "Open the pull request this agent recorded.",
				Timeout:     45 * time.Second,
			},
		},
		{
			name:     "a shebang is passed over rather than ending the header",
			file:     "after-shebang",
			wantText: true,
			want: Header{
				Description: "Declares itself under a shebang.",
				Timeout:     2 * time.Minute,
			},
		},
		{
			name:     "ordinary comments and blank lines are passed over too",
			file:     "interleaved-comments",
			wantText: true,
			want: Header{
				Description: "Declared among ordinary comments.",
				Timeout:     90 * time.Second,
			},
		},
		{
			name:     "slashes start a comment as readily as a hash",
			file:     "slash-comments",
			wantText: true,
			want: Header{
				Description: "Declared in a language whose comments are slashes.",
				Timeout:     time.Minute,
			},
		},
		{
			name:     "none declares no limit rather than leaving the timeout unset",
			file:     "unlimited-timeout",
			wantText: true,
			want: Header{
				Description: "Declares that it must never be timed out.",
				Timeout:     unlimitedTimeout,
			},
		},
	}

	described := make([]string, 0, len(tests))
	for _, tt := range tests {
		described = append(described, tt.file)

		t.Run(tt.name, func(t *testing.T) {
			header, text := readHeader(filepath.Join(fixtureDir, tt.file))

			assert.Equal(t, tt.want, header)
			assert.Equal(t, tt.wantText, text)
		})
	}

	entries, err := os.ReadDir(fixtureDir)
	require.NoError(t, err)

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		files = append(files, entry.Name())
	}

	assert.ElementsMatch(t, files, described, "every file in %s wants a row of its own", fixtureDir)
}

// TestReadHeaderUnreadable covers a file weave cannot open, which reads as no header rather
// than an error, since Discover offers the file either way. It is not text either, since
// nothing was read to say otherwise.
func TestReadHeaderUnreadable(t *testing.T) {
	header, text := readHeader(filepath.Join(t.TempDir(), "not-there"))

	assert.Equal(t, Header{}, header)
	assert.False(t, text)
}

func TestHeaderTimeoutOr(t *testing.T) {
	tests := []struct {
		name     string
		header   Header
		fallback time.Duration
		want     time.Duration
	}{
		{
			name:     "a header that declares nothing falls back",
			header:   Header{},
			fallback: 2 * time.Minute,
			want:     2 * time.Minute,
		},
		{
			name:     "a header that declares no limit beats a real fallback",
			header:   Header{Timeout: unlimitedTimeout},
			fallback: 2 * time.Minute,
			want:     0,
		},
		{
			name:     "a header that declares a duration is held to it",
			header:   Header{Timeout: 30 * time.Second},
			fallback: 2 * time.Minute,
			want:     30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.header.TimeoutOr(tt.fallback))
		})
	}
}
