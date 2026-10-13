package checklog_test

import (
	"strings"
	"testing"
	"time"

	"komrad/internal/checklog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func at(t *testing.T, stamp string) *time.Time {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, stamp)
	require.NoError(t, err)

	return &parsed
}

// TestParseCleansLines covers that parsing strips timestamps and escape codes from each line and counts errors.
func TestParseCleansLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		raw        string
		want       string
		wantCount  int
		wantErrors int
	}{
		{
			name:      "an empty log has no lines",
			raw:       "",
			want:      "",
			wantCount: 0,
		},
		{
			name:      "timestamps with any fractional digits are stripped",
			raw:       "2026-09-20T12:00:00Z one\n2026-09-20T12:00:00.1234567Z two\n2026-09-20T12:00:00.123456789012Z three\n",
			want:      "one\ntwo\nthree\n",
			wantCount: 3,
		},
		{
			name:      "a line with no timestamp is kept whole",
			raw:       "2026-09-20T12:00:00Z one\ncontinued here\n",
			want:      "one\ncontinued here\n",
			wantCount: 2,
		},
		{
			name:      "the byte order mark is stripped only at the start",
			raw:       "\ufeff2026-09-20T12:00:00Z one\n\ufefftwo\n",
			want:      "one\n\ufefftwo\n",
			wantCount: 2,
		},
		{
			name:      "a carriage return keeps what follows the last one",
			raw:       "2026-09-20T12:00:00Z 10%\r50%\r100%\r\n2026-09-20T12:00:01Z done\r\n",
			want:      "100%\ndone\n",
			wantCount: 2,
		},
		{
			name:      "escape codes are removed",
			raw:       "2026-09-20T12:00:00Z \x1b[31;1mred\x1b[0m and \x1b]8;;https://x\x07link\x1b]8;;\x1b\\ done\n",
			want:      "red and link done\n",
			wantCount: 1,
		},
		{
			name:      "text after the last newline is a line",
			raw:       "2026-09-20T12:00:00Z one\n2026-09-20T12:00:01Z two",
			want:      "one\ntwo\n",
			wantCount: 2,
		},
		{
			name:       "lines the job marked as errors are counted",
			raw:        "2026-09-20T12:00:00Z ##[error]boom\n2026-09-20T12:00:00Z not ##[error] here\n2026-09-20T12:00:00Z \x1b[31m##[error]again\n",
			want:       "##[error]boom\nnot ##[error] here\n##[error]again\n",
			wantCount:  3,
			wantErrors: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log, err := checklog.Parse(strings.NewReader(tt.raw), nil)
			require.NoError(t, err)
			assert.Equal(t, tt.want, log.Text)
			assert.Equal(t, tt.wantCount, log.LineCount)
			assert.Len(t, checklog.Lines(log.Text), tt.wantCount)
			assert.Equal(t, tt.wantErrors, log.ErrorCount)
			assert.Empty(t, log.Steps)
		})
	}
}

// TestParseMapsSteps covers which lines each step holds.
func TestParseMapsSteps(t *testing.T) {
	t.Parallel()

	raw := strings.Join([]string{
		"2026-09-20T12:00:00.5Z Current runner version",
		"2026-09-20T12:00:01Z ##[group]Operating System",
		"2026-09-20T12:00:01Z ##[endgroup]",
		"2026-09-20T12:00:02.9Z ##[group]Run actions/checkout@v4",
		"2026-09-20T12:00:03Z Syncing repository",
		"2026-09-20T12:00:03Z ##[endgroup]",
		"2026-09-20T12:00:05Z ##[group]Run go test ./...",
		"2026-09-20T12:00:05Z ##[endgroup]",
		"2026-09-20T12:00:06Z FAIL",
		"2026-09-20T12:00:06Z ##[error]Process completed with exit code 1.",
		"2026-09-20T12:00:08Z Post job cleanup.",
	}, "\n") + "\n"

	steps := []checklog.Step{
		{
			Number:     1,
			Name:       "Set up job",
			Conclusion: "success",
			StartedAt:  at(t, "2026-09-20T12:00:01Z"),
		},
		{
			Number:     2,
			Name:       "Run actions/checkout@v4",
			Conclusion: "success",
			StartedAt:  at(t, "2026-09-20T12:00:03Z"),
		},
		{
			Number:     3,
			Name:       "Lint",
			Conclusion: "skipped",
		},
		{
			Number:     4,
			Name:       "Run go test ./...",
			Conclusion: "failure",
			StartedAt:  at(t, "2026-09-20T12:00:05Z"),
		},
		{
			Number:     5,
			Name:       "Complete job",
			Conclusion: "success",
			StartedAt:  at(t, "2026-09-20T12:00:08Z"),
		},
	}

	log, err := checklog.Parse(strings.NewReader(raw), steps)
	require.NoError(t, err)
	assert.Equal(t, 11, log.LineCount)
	assert.Equal(t, 1, log.ErrorCount)
	assert.Equal(t, []checklog.StepRange{
		{
			Number:     1,
			Name:       "Set up job",
			Conclusion: "success",
			FirstLine:  1,
			LineCount:  3,
		},
		{
			Number:     2,
			Name:       "Run actions/checkout@v4",
			Conclusion: "success",
			FirstLine:  4,
			LineCount:  3,
		},
		{
			Number:     4,
			Name:       "Run go test ./...",
			Conclusion: "failure",
			FirstLine:  7,
			LineCount:  4,
			ErrorCount: 1,
		},
		{
			Number:     5,
			Name:       "Complete job",
			Conclusion: "success",
			FirstLine:  11,
			LineCount:  1,
		},
	}, log.Steps)
}

// TestParseFindsRenamedStepsByTheFirstGroup covers that a step named apart from its group begins at the first group after it started.
func TestParseFindsRenamedStepsByTheFirstGroup(t *testing.T) {
	t.Parallel()

	raw := strings.Join([]string{
		"2026-09-20T12:00:00Z setup",
		"2026-09-20T12:00:02Z noise before the group",
		"2026-09-20T12:00:02Z ##[group]Run make build",
		"2026-09-20T12:00:02Z ##[group]Run nested",
		"2026-09-20T12:00:04Z ##[group]Run make test",
		"2026-09-20T12:00:04Z ok",
	}, "\n") + "\n"

	steps := []checklog.Step{
		{
			Number:    1,
			Name:      "Set up job",
			StartedAt: at(t, "2026-09-20T12:00:00Z"),
		},
		{
			Number:    2,
			Name:      "Build",
			StartedAt: at(t, "2026-09-20T12:00:02Z"),
		},
		{
			Number:    3,
			Name:      "Run make test",
			StartedAt: at(t, "2026-09-20T12:00:04Z"),
		},
	}

	log, err := checklog.Parse(strings.NewReader(raw), steps)
	require.NoError(t, err)
	assert.Equal(t, []checklog.StepRange{
		{
			Number:    1,
			Name:      "Set up job",
			FirstLine: 1,
			LineCount: 2,
		},
		{
			Number:    2,
			Name:      "Build",
			FirstLine: 3,
			LineCount: 2,
		},
		{
			Number:    3,
			Name:      "Run make test",
			FirstLine: 5,
			LineCount: 2,
		},
	}, log.Steps)
}

// TestParseLeavesLinesBeforeTheFirstStepInNone covers that lines before any step started sit in no step.
func TestParseLeavesLinesBeforeTheFirstStepInNone(t *testing.T) {
	t.Parallel()

	raw := "preamble with no stamp\n2026-09-20T12:00:05Z ##[group]Run go test\n2026-09-20T12:00:05Z ok\n"
	steps := []checklog.Step{
		{
			Number:    1,
			Name:      "Run go test",
			StartedAt: at(t, "2026-09-20T12:00:05Z"),
		},
		{
			Number:    2,
			Name:      "Never logged",
			StartedAt: at(t, "2026-09-20T12:00:09Z"),
		},
	}

	log, err := checklog.Parse(strings.NewReader(raw), steps)
	require.NoError(t, err)
	assert.Equal(t, []checklog.StepRange{
		{
			Number:    1,
			Name:      "Run go test",
			FirstLine: 2,
			LineCount: 2,
		},
		{
			Number:    2,
			Name:      "Never logged",
			FirstLine: 4,
			LineCount: 0,
		},
	}, log.Steps)
}
