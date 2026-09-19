package doctor

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"weave/internal/view"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// measuring is an env whose terminal draws every glyph in the columns given and every
// calibration character in the width nobody disputes.
func measuring(t *testing.T, glyphColumns int) Env {
	t.Helper()

	return Env{
		MeasureColumns: func(_ context.Context, samples []string) ([]int, error) {
			require.Len(t, samples, len(calibration)+len(view.Drawings()))

			columns := make([]int, 0, len(samples))
			for _, drawn := range calibration {
				columns = append(columns, drawn.Columns)
			}
			for range view.Drawings() {
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
			for _, drawn := range append(slices.Clone(calibration), view.Drawings()...) {
				columns = append(columns, drawn.Columns)
			}

			return columns, nil
		},
	}
}

// TestGlyphCheckWarnsWhenTheTerminalWouldNotYield covers a real terminal that refused raw mode,
// which is a person's problem and must not be buried in the same skip as having no terminal.
func TestGlyphCheckWarnsWhenTheTerminalWouldNotYield(t *testing.T) {
	env := Env{MeasureColumns: func(context.Context, []string) ([]int, error) {
		return nil, fmt.Errorf("%w: %w", ErrTerminalWouldNotYield, errors.New("inappropriate ioctl"))
	}}

	result := newTerminalGlyphCheck().Run(t.Context(), env)
	require.Equal(t, StatusWarn, result.Status)
	assert.Contains(t, result.Err.Error(), "inappropriate ioctl")
}

// TestGlyphCheckDoesNotBlameTheFont covers the fix text, since a cursor advance says nothing
// about whether a glyph is in the font and telling a person to install one would be a guess.
func TestGlyphCheckDoesNotBlameTheFont(t *testing.T) {
	result := newTerminalGlyphCheck().Run(t.Context(), measuring(t, 2))

	require.Equal(t, StatusWarn, result.Status)
	assert.NotContains(t, strings.ToLower(result.Fix), "font")
}

// TestGlyphCheckPassesWhenTheWidthsAgree covers the ordinary machine, where the constant the
// layout rests on is what the terminal does.
func TestGlyphCheckPassesWhenTheWidthsAgree(t *testing.T) {
	result := newTerminalGlyphCheck().Run(t.Context(), agreeing())
	require.Equal(t, StatusOK, result.Status, result.Detail)
	assert.Contains(t, result.Detail, "glyph")
}

// TestGlyphCheckReportsWidthsThatDisagree covers the whole reason this exists, a terminal
// drawing an Octicon in a width the layout does not count it as.
func TestGlyphCheckReportsWidthsThatDisagree(t *testing.T) {
	result := newTerminalGlyphCheck().Run(t.Context(), measuring(t, 2))

	require.Equal(t, StatusWarn, result.Status,
		"a font the terminal lacks is a degraded panel rather than a broken weave")
	require.ErrorIs(t, result.Err, ErrGlyphWidthUnexpected)
	assert.Contains(t, result.Err.Error(), "19 glyphs drew in 2 columns where the layout counts them as 1")
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
	assert.NotContains(t, result.Err.Error(), "unverified",
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

	disagreeing := measuring(t, 2)
	disagreeing.InsideTmux = env.InsideTmux

	warned := newTerminalGlyphCheck().Run(t.Context(), disagreeing)
	require.Equal(t, StatusWarn, warned.Status)
	assert.Contains(t, warned.Err.Error(), "tmux",
		"the text report shows the error rather than the detail, so the caveat has to be in both")
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

func terminfo(answers map[string]string) func(context.Context, string, string) (string, error) {
	return func(_ context.Context, _, capability string) (string, error) {
		return answers[capability], nil
	}
}

// naming is an env inside tmux whose tmux names the terminal it paints to and the entry it
// advertises to what it runs.
func naming(client, defaultTerminal string, answers map[string]string) Env {
	return Env{
		Settings:     Settings{Term: "tmux-256color", InsideTmux: "/tmp/tmux-501/default,1,0"},
		ReadTerminfo: terminfo(answers),
		Output: func(_ context.Context, _ string, args ...string) (string, error) {
			if slices.Contains(args, "default-terminal") {
				return defaultTerminal, nil
			}

			return client, nil
		},
	}
}

// italicCapable is a terminfo entry naming both escapes.
func italicCapable() map[string]string {
	return map[string]string{"bold": "\x1b[1m", "sitm": "\x1b[3m"}
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
		Settings:     Settings{Term: "xterm-256color"},
		ReadTerminfo: terminfo(map[string]string{"bold": "\x1b[1m", "sitm": "\x1b[3m"}),
	}

	assert.Equal(t, StatusOK, newTerminalEmphasisCheck().Run(t.Context(), env).Status)
}

// TestEmphasisCheckAsksTmuxRatherThanItsOwnTerm covers the environment weave always runs in,
// where the entry tmux advertises to what it runs answers for nothing a person can see.
func TestEmphasisCheckAsksTmuxRatherThanItsOwnTerm(t *testing.T) {
	env := naming("xterm-256color", "tmux-256color", italicCapable())

	result := newTerminalEmphasisCheck().Run(t.Context(), env)
	require.Equal(t, StatusOK, result.Status, result.Detail)
	assert.Contains(t, result.Detail, "xterm-256color")
	assert.NotContains(t, result.Detail, "tmux-256color",
		"tmux-256color is what the panel emits for, not what paints, so quoting it would mislead")
}

// TestEmphasisCheckSkipsWhenTmuxNamesNoClient covers a detached session, where nothing can be
// said about the painting terminal and saying it is missing italics would be a lie.
func TestEmphasisCheckSkipsWhenTmuxNamesNoClient(t *testing.T) {
	env := naming("", "tmux-256color", nil)

	result := newTerminalEmphasisCheck().Run(t.Context(), env)
	require.Equal(t, StatusSkip, result.Status)
	assert.Contains(t, result.Detail, "nothing is attached to this session")
}

// TestEmphasisCheckCatchesTmuxRefusingItalics covers a terminal that draws italics perfectly
// well behind a tmux that will not send them, which no terminfo entry on its own reveals.
func TestEmphasisCheckCatchesTmuxRefusingItalics(t *testing.T) {
	env := naming("xterm-256color", "screen-256color", italicCapable())

	result := newTerminalEmphasisCheck().Run(t.Context(), env)
	require.Equal(t, StatusWarn, result.Status, result.Detail)
	require.ErrorIs(t, result.Err, ErrItalicSubstituted)
	assert.Contains(t, result.Err.Error(), "drawn reversed rather than italic")
	assert.Contains(t, result.Fix, "default-terminal")
}

// TestEmphasisCheckSaysAChildIsReversedInTmux covers the shape of the loss, since tmux
// substitutes standout rather than dropping the attribute and a reversed row reads as selected.
func TestEmphasisCheckSaysAChildIsReversedInTmux(t *testing.T) {
	inside := naming("vt100", "tmux-256color", map[string]string{"bold": "\x1b[1m"})

	result := newTerminalEmphasisCheck().Run(t.Context(), inside)
	require.Equal(t, StatusWarn, result.Status)
	assert.Contains(t, result.Err.Error(), "drawn reversed rather than italic")

	outside := Env{
		Settings:     Settings{Term: "vt100"},
		ReadTerminfo: terminfo(map[string]string{"bold": "\x1b[1m"}),
	}

	plain := newTerminalEmphasisCheck().Run(t.Context(), outside)
	require.Equal(t, StatusWarn, plain.Status)
	assert.Contains(t, plain.Err.Error(), "a child reads as an ordinary agent",
		"with no tmux in the way the attribute is dropped rather than substituted")
}

// TestEmphasisCheckReportsBothFaultsAtOnce covers a terminal naming no bold behind a tmux that
// refuses italics, where reporting either fault alone leaves the other one unsaid.
func TestEmphasisCheckReportsBothFaultsAtOnce(t *testing.T) {
	env := naming("xterm-256color", "screen-256color", map[string]string{"sitm": "\x1b[3m"})

	result := newTerminalEmphasisCheck().Run(t.Context(), env)
	require.Equal(t, StatusWarn, result.Status, result.Detail)
	assert.Contains(t, result.Err.Error(), "a workflow row reads as an ordinary agent")
	assert.Contains(t, result.Err.Error(), "drawn reversed rather than italic")
	assert.Contains(t, result.Fix, "TERM")
	assert.Contains(t, result.Fix, "default-terminal")
}

// TestEmphasisCheckSkipsWhenTmuxWillNotSayWhatItAdvertises covers the same unreachable tmux the
// painting term is asked about, which must not be a skip there and a warn here.
func TestEmphasisCheckSkipsWhenTmuxWillNotSayWhatItAdvertises(t *testing.T) {
	env := naming("xterm-256color", "tmux-256color", italicCapable())
	env.Output = func(_ context.Context, _ string, args ...string) (string, error) {
		if slices.Contains(args, "default-terminal") {
			return "", errors.New("no server running")
		}

		return "xterm-256color", nil
	}

	result := newTerminalEmphasisCheck().Run(t.Context(), env)
	require.Equal(t, StatusSkip, result.Status)
	assert.Contains(t, result.Detail, ErrDefaultTerminalUnknown.Message)
}

// TestDoesTmuxSubstituteItalics covers which entries tmux refuses italics on, every row of it
// measured by capturing what tmux writes to an attached client.
func TestDoesTmuxSubstituteItalics(t *testing.T) {
	for _, tt := range []struct {
		name    string
		entry   string
		refused bool
	}{
		{
			name:    "the bare screen entry",
			entry:   "screen",
			refused: true,
		},
		{
			name:    "a hyphenated screen entry",
			entry:   "screen-256color",
			refused: true,
		},
		{
			name:  "a dotted screen entry, which tmux sends italics on",
			entry: "screen.xterm-256color",
		},
		{
			name:  "an entry naming no sitm of its own",
			entry: "ansi",
		},
		{
			name:  "the entry weave would want",
			entry: "tmux-256color",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := naming("xterm-256color", tt.entry, italicCapable())

			refused, err := doesTmuxSubstituteItalics(t.Context(), env)
			require.NoError(t, err)
			assert.Equal(t, tt.refused, refused)
		})
	}
}

// TestDoesTmuxSubstituteItalicsOutsideTmux covers there being no tmux to ask, where the escape
// reaches the terminal unaltered whatever any entry says.
func TestDoesTmuxSubstituteItalicsOutsideTmux(t *testing.T) {
	refused, err := doesTmuxSubstituteItalics(t.Context(), Env{Settings: Settings{Term: "xterm-256color"}})

	require.NoError(t, err)
	assert.False(t, refused)
}

// TestEmphasisCheckWithoutTerm covers a shell that names no terminal, where terminfo has
// nothing to be asked about.
func TestEmphasisCheckWithoutTerm(t *testing.T) {
	result := newTerminalEmphasisCheck().Run(t.Context(), Env{})

	assert.Equal(t, StatusSkip, result.Status)
	assert.Contains(t, result.Detail, ErrTermUnset.Message)
}

// TestEmphasisCheckWhenTerminfoWillNotAnswer covers a machine with no terminfo reader on it,
// which says nothing about the terminal either way.
func TestEmphasisCheckWhenTerminfoWillNotAnswer(t *testing.T) {
	env := Env{
		Settings: Settings{Term: "xterm-256color"},
		ReadTerminfo: func(context.Context, string, string) (string, error) {
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
		{
			name:    "too shallow",
			colors:  "8",
			status:  StatusWarn,
			problem: ErrColorTooShallow,
		},
		{
			name:    "not a number",
			colors:  "lots",
			status:  StatusWarn,
			problem: ErrTerminfoUnreadable,
		},
		{
			name:   "deep enough",
			colors: "256",
			status: StatusOK,
		},
		{
			name:   "deeper still",
			colors: "16777216",
			status: StatusOK,
		},
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
