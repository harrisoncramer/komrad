package doctor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// measuring is an env whose terminal draws every glyph in the columns given and every
// calibration character in the width nobody disputes.
func measuring(t *testing.T, glyphColumns int) Env {
	t.Helper()

	return Env{
		MeasureColumns: func(_ context.Context, samples []string) ([]int, error) {
			require.Len(t, samples, len(calibration)+len(panelGlyphs()))

			columns := make([]int, 0, len(samples))
			for _, drawn := range calibration {
				columns = append(columns, drawn.Columns)
			}
			for range panelGlyphs() {
				columns = append(columns, glyphColumns)
			}

			return columns, nil
		},
	}
}

// TestGlyphCheckSkipsWithNoTerminal covers the failure most likely to bite, which is doctor
// being piped or run by a test and a cursor query hanging on something that answers nothing.
func TestGlyphCheckSkipsWithNoTerminal(t *testing.T) {
	result := newTerminalGlyphCheck().Run(t.Context(), Env{})

	assert.Equal(t, StatusSkip, result.Status)
	assert.Contains(t, result.Detail, "terminal")
	assert.NoError(t, result.Err, "a skip says why it was skipped rather than reporting a fault") //nolint:testifylint // the run carries on either way, so this is not the assertion the test turns on
}

// TestGlyphCheckSkipsWhenTheTerminalWalkedAway covers the probe finding no tty of its own,
// which is the same nothing-to-ask-about as having none wired.
func TestGlyphCheckSkipsWhenTheTerminalWalkedAway(t *testing.T) {
	env := Env{MeasureColumns: func(context.Context, []string) ([]int, error) { return nil, ErrNoTerminal }}

	result := newTerminalGlyphCheck().Run(t.Context(), env)
	assert.Equal(t, StatusSkip, result.Status)
}

// agreeing is an env whose terminal draws everything in the width the layout counts it as.
func agreeing() Env {
	return Env{
		MeasureColumns: func(_ context.Context, samples []string) ([]int, error) {
			columns := make([]int, 0, len(samples))
			for _, drawn := range append(append([]Drawn{}, calibration...), panelGlyphs()...) {
				columns = append(columns, drawn.Columns)
			}

			return columns, nil
		},
	}
}

// TestGlyphCheckPassesWhenTheWidthsAgree covers the ordinary machine, where the constant the
// layout rests on is what the terminal does.
func TestGlyphCheckPassesWhenTheWidthsAgree(t *testing.T) {
	result := newTerminalGlyphCheck().Run(t.Context(), agreeing())
	require.Equal(t, StatusOK, result.Status, result.Detail)
	assert.Contains(t, result.Detail, "columns")
}

// TestGlyphCheckReportsWidthsThatDisagree covers the whole reason this exists, a terminal
// drawing an Octicon in a width the layout does not count it as.
func TestGlyphCheckReportsWidthsThatDisagree(t *testing.T) {
	result := newTerminalGlyphCheck().Run(t.Context(), measuring(t, 1))

	require.Equal(t, StatusWarn, result.Status,
		"a font the terminal lacks is a degraded panel rather than a broken weave")
	require.ErrorIs(t, result.Err, ErrGlyphWidthUnexpected)
	assert.Contains(t, result.Err.Error(), "repo drew in 1, counted as 2")
	assert.NotEmpty(t, result.Fix)
}

// TestGlyphCheckDistrustsATerminalThatMisreadsPlainText covers the reading being thrown away
// rather than reported, since a terminal wrong about x is not worth quoting about an Octicon.
func TestGlyphCheckDistrustsATerminalThatMisreadsPlainText(t *testing.T) {
	env := Env{
		MeasureColumns: func(_ context.Context, samples []string) ([]int, error) {
			columns := make([]int, len(samples))
			for index := range columns {
				columns[index] = 3
			}

			return columns, nil
		},
	}

	result := newTerminalGlyphCheck().Run(t.Context(), env)
	require.Equal(t, StatusWarn, result.Status)
	require.ErrorIs(t, result.Err, ErrCursorQueryUntrusted)
	assert.NotContains(t, result.Err.Error(), "review",
		"a calibration that failed stops the reading rather than colouring it with glyph widths")
}

// TestGlyphCheckSaysWhenItMeasuredTmux covers the caveat that decides what the number means,
// since tmux answers the query off its own grid rather than the terminal's.
func TestGlyphCheckSaysWhenItMeasuredTmux(t *testing.T) {
	env := agreeing()
	env.InsideTmux = "/tmp/tmux-501/default,1,0"

	result := newTerminalGlyphCheck().Run(t.Context(), env)
	require.Equal(t, StatusOK, result.Status)
	assert.Contains(t, result.Detail, "tmux")
}

// TestGlyphCheckRefusesAShortAnswer covers a terminal answering about fewer glyphs than it was
// asked about, which would otherwise be read as the wrong glyphs being the wrong width.
func TestGlyphCheckRefusesAShortAnswer(t *testing.T) {
	env := Env{
		MeasureColumns: func(context.Context, []string) ([]int, error) { return []int{1, 2}, nil },
	}

	result := newTerminalGlyphCheck().Run(t.Context(), env)
	assert.Equal(t, StatusWarn, result.Status)
	assert.ErrorIs(t, result.Err, ErrCursorQueryUnanswered)
}

func terminfo(answers map[string]string) func(context.Context, string) (string, error) {
	return func(_ context.Context, capability string) (string, error) {
		return answers[capability], nil
	}
}

// TestEmphasisCheckNamesWhatIsLost covers the point of the check, which is that a person told
// only that sitm is missing has learned nothing about their panel.
func TestEmphasisCheckNamesWhatIsLost(t *testing.T) {
	env := Env{
		Settings:     Settings{Term: "screen-256color"},
		ReadTerminfo: terminfo(map[string]string{"bold": "\x1b[1m"}),
	}

	result := newTerminalEmphasisCheck().Run(t.Context(), env)
	require.Equal(t, StatusWarn, result.Status)
	require.ErrorIs(t, result.Err, ErrEmphasisMissing)
	assert.Contains(t, result.Err.Error(), "a child reads as an ordinary agent")
	assert.NotContains(t, result.Err.Error(), "a workflow row reads")
}

// TestEmphasisCheckPasses covers the terminal that can letter a row both ways.
func TestEmphasisCheckPasses(t *testing.T) {
	env := Env{
		Settings:     Settings{Term: "tmux-256color"},
		ReadTerminfo: terminfo(map[string]string{"bold": "\x1b[1m", "sitm": "\x1b[3m"}),
	}

	assert.Equal(t, StatusOK, newTerminalEmphasisCheck().Run(t.Context(), env).Status)
}

// TestEmphasisCheckWithoutTerm covers a shell that names no terminal, where terminfo has
// nothing to be asked about.
func TestEmphasisCheckWithoutTerm(t *testing.T) {
	result := newTerminalEmphasisCheck().Run(t.Context(), Env{})

	assert.Equal(t, StatusWarn, result.Status)
	assert.ErrorIs(t, result.Err, ErrTermUnset)
}

// TestEmphasisCheckWhenTerminfoWillNotAnswer covers a machine with no terminfo reader on it,
// which says nothing about the terminal either way.
func TestEmphasisCheckWhenTerminfoWillNotAnswer(t *testing.T) {
	env := Env{
		Settings: Settings{Term: "tmux-256color"},
		ReadTerminfo: func(context.Context, string) (string, error) {
			return "", errors.New("tput: not found")
		},
	}

	result := newTerminalEmphasisCheck().Run(t.Context(), env)
	assert.Equal(t, StatusWarn, result.Status)
	assert.ErrorIs(t, result.Err, ErrTerminfoUnreadable)
}

// TestColorCheck covers the depth terminfo claims against the depth the palette is mixed for.
func TestColorCheck(t *testing.T) {
	for _, tt := range []struct {
		name    string
		colors  string
		status  Status
		problem *ProblemError
	}{
		{name: "deep enough", colors: "256", status: StatusOK},
		{name: "deeper still", colors: "16777216", status: StatusOK},
		{name: "too shallow", colors: "8", status: StatusWarn, problem: ErrColorTooShallow},
		{name: "not a number", colors: "lots", status: StatusWarn, problem: ErrTerminfoUnreadable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := Env{
				Settings:     Settings{Term: "some-term", ColorTerm: "truecolor"},
				ReadTerminfo: terminfo(map[string]string{"colors": tt.colors}),
			}

			result := newTerminalColorCheck().Run(t.Context(), env)
			require.Equal(t, tt.status, result.Status, result.Detail)
			if tt.problem != nil {
				assert.ErrorIs(t, result.Err, tt.problem)
			}
		})
	}
}

// TestColorCheckQuotesColorterm covers the one thing terminfo cannot say, since a 256-colour
// entry on a terminal announcing truecolor is the ordinary case rather than a shortfall.
func TestColorCheckQuotesColorterm(t *testing.T) {
	env := Env{
		Settings:     Settings{Term: "tmux-256color", ColorTerm: "truecolor"},
		ReadTerminfo: terminfo(map[string]string{"colors": "256"}),
	}

	assert.Contains(t, newTerminalColorCheck().Run(t.Context(), env).Detail, "truecolor")
}
