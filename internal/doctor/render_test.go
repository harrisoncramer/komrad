package doctor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sample() Report {
	findings := []Finding{
		{
			Name:   "tmux",
			Status: StatusOK,
			Detail: "/usr/bin/tmux",
		},
		{
			Name: "tmux version", Status: StatusFail, Detail: "tmux 3.1c",
			Code: "tmux_too_old", Error: "tmux is too old: found tmux 3.1c",
			Fix: "upgrade tmux",
		},
		{
			Name:   "db",
			Status: StatusSkip,
			Detail: "not checked, tmux has to pass first",
		},
		{
			Name:   "codex",
			Status: StatusWarn,
			Detail: "not on PATH",
			Error:  "codex is not on PATH, so only agents set to it are affected",
		},
	}

	return Report{Findings: findings, Summary: summarize(findings)}
}

// TestWriteTextColumnsLineUp covers the property that makes a list of a dozen checks
// scannable: every detail starts in the same column whatever the status and name were.
func TestWriteTextColumnsLineUp(t *testing.T) {
	report := sample()

	var out strings.Builder
	require.NoError(t, WriteText(&out, report, false))
	rendered := out.String()

	columns := make(map[string]int, len(report.Findings))
	for _, finding := range report.Findings {
		var row string
		for _, candidate := range strings.Split(rendered, "\n") {
			if strings.Contains(candidate, finding.Name) && !strings.Contains(candidate, "fix:") {
				row = candidate

				break
			}
		}
		require.NotEmpty(t, row, "every finding is drawn on a line of its own: %s", finding.Name)

		column := strings.LastIndex(row, describeFinding(finding))
		require.NotEqual(t, -1, column, "the finding's text is on its line: %s", finding.Name)
		columns[finding.Name] = column
	}

	require.Len(t, columns, len(report.Findings))
	for name, column := range columns {
		assert.Equal(t, columns[report.Findings[0].Name], column,
			"every finding's text starts in the same column, including %s", name)
	}
}

// TestWriteTextCarriesAFixOnlyWhereThereIsSomethingToDo covers that a fix is an
// instruction: it appears under what is wrong and nowhere else.
func TestWriteTextCarriesAFixOnlyWhereThereIsSomethingToDo(t *testing.T) {
	var out strings.Builder
	require.NoError(t, WriteText(&out, sample(), false))

	assert.Equal(t, 1, strings.Count(out.String(), "fix:"))
	assert.Contains(t, out.String(), "fix: upgrade tmux")
}

// TestWriteTextSaysWhatFailed covers the last line, which is what a reader takes away when
// they do not read the list.
func TestWriteTextSaysWhatFailed(t *testing.T) {
	tests := []struct {
		name     string
		findings []Finding
		want     string
	}{
		{
			name: "something wrong",
			findings: []Finding{
				{Name: "tmux", Status: StatusOK},
				{
					Name:   "db",
					Status: StatusFail,
					Error:  "unreachable",
				},
			},
			want: "1 of 2 checks failed.",
		},
		{
			name: "a warning alongside",
			findings: []Finding{
				{
					Name:   "db",
					Status: StatusFail,
					Error:  "unreachable",
				},
				{
					Name:   "codex",
					Status: StatusWarn,
					Error:  "not on PATH",
				},
			},
			want: "1 of 2 checks failed, 1 worth a look.",
		},
		{
			name:     "nothing wrong",
			findings: []Finding{{Name: "tmux", Status: StatusOK}},
			want:     "1 checks, all ok.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			report := Report{Findings: tt.findings, Summary: summarize(tt.findings)}
			require.NoError(t, WriteText(&out, report, false))

			assert.Contains(t, out.String(), tt.want)
		})
	}
}

// TestWriteTextColorIsOptional covers that the plain output carries no escapes, since it
// is read as often through a pipe as in a terminal.
func TestWriteTextColorIsOptional(t *testing.T) {
	var plain, colored strings.Builder
	require.NoError(t, WriteText(&plain, sample(), false))
	require.NoError(t, WriteText(&colored, sample(), true))

	assert.NotContains(t, plain.String(), "\033[")
	assert.Contains(t, colored.String(), "\033[")
}

// TestWriteJSONIsTheWholeReport covers that the machine-readable output keeps the code a
// script switches on, which the rendered line does not show.
func TestWriteJSONIsTheWholeReport(t *testing.T) {
	var out strings.Builder
	require.NoError(t, WriteJSON(&out, sample()))

	var decoded Report
	require.NoError(t, json.Unmarshal([]byte(out.String()), &decoded))

	assert.Equal(t, sample(), decoded)
	assert.Contains(t, out.String(), `"code": "tmux_too_old"`)
	assert.False(t, decoded.Summary.Healthy)
}
