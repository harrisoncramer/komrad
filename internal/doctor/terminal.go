package doctor

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"weave/internal/view"
	"weave/lib/xslice"
)

// paletteColors is the number of colours the panel's palette is mixed for.
const paletteColors = 256

var (
	ErrNoTerminal = &ProblemError{
		Code:    "no_terminal",
		Message: "this is not running on a terminal, so nothing could be asked of one",
	}
	ErrTerminalWouldNotYield = &ProblemError{
		Code:    "terminal_would_not_yield",
		Message: "the terminal would not be put into raw mode, so its answer could not be read back",
	}
	ErrCursorQueryUnanswered = &ProblemError{
		Code:    "cursor_query_unanswered",
		Message: "the terminal did not answer a cursor position query, so no width was measured",
	}
	ErrCursorQueryUntrusted = &ProblemError{
		Code:    "cursor_query_untrusted",
		Message: "the terminal misreported the width of a character whose width is not in doubt",
	}
	ErrGlyphWidthUnexpected = &ProblemError{
		Code:    "glyph_width_unexpected",
		Message: "the terminal advances the cursor a different number of columns for a glyph than the list's layout counts it as",
	}
	ErrTermUnset = &ProblemError{
		Code:    "term_unset",
		Message: "TERM names no terminal, so terminfo has nothing to answer about",
	}
	ErrPaintingTermUnknown = &ProblemError{
		Code:    "painting_term_unknown",
		Message: "tmux would not name the terminal it paints to, and the entry it advertises inside itself answers for nothing a person sees",
	}
	ErrTerminfoUnreadable = &ProblemError{
		Code:    "terminfo_unreadable",
		Message: "terminfo could not be read, so what this terminal can draw is unknown",
	}
	ErrEmphasisMissing = &ProblemError{
		Code:    "emphasis_missing",
		Message: "terminfo names no escape for lettering the list uses to say what a row is",
	}
	ErrColorTooShallow = &ProblemError{
		Code:    "color_too_shallow",
		Message: "terminfo reports fewer colours than the palette is written for",
	}
)

// calibration is characters of undisputed width, measured alongside the glyphs so a terminal
// answering nonsense is caught before any of its other answers is believed.
var calibration = []view.Drawn{
	{Name: "latin x", Text: "x", Columns: 1},
	{Name: "han character", Text: "漢", Columns: 2},
}

// emphasis is the terminfo name of each escape the list letters a row with, and what is lost
// when the entry names none.
var emphasis = []struct {
	capability string
	loss       string
}{
	{capability: "bold", loss: "a workflow row reads as an ordinary agent"},
	{capability: "sitm", loss: "a child reads as an ordinary agent"},
}

// newTerminalGlyphCheck measures what the terminal draws each of the list's glyphs in, which is
// the one thing here that is measured rather than inferred.
func newTerminalGlyphCheck() Check {
	return Check{
		Name:        "terminal glyphs",
		Description: "the list's glyphs are drawn in the columns its layout counts them as",
		Run: func(ctx context.Context, env Env) Result {
			if env.MeasureColumns == nil {
				return Result{Status: StatusSkip, Detail: "not checked, nothing is attached to a terminal to ask"}
			}

			drawings := append(slices.Clone(calibration), view.Drawings()...)

			measured, err := env.MeasureColumns(ctx, xslice.Map(drawings, textOf))
			if err != nil {
				if codeOf(err) == ErrNoTerminal.Code {
					return Result{Status: StatusSkip, Detail: "not checked, " + err.Error()}
				}

				return warn(err, "")
			}

			if len(measured) != len(drawings) {
				return warn(fmt.Errorf("%w: asked about %d, heard back about %d",
					ErrCursorQueryUnanswered, len(drawings), len(measured)), "")
			}

			for index, drawn := range calibration {
				if measured[index] != drawn.Columns {
					return warn(fmt.Errorf("%w: %s drew in %d columns rather than %d, so the rest of the reading is not worth quoting",
						ErrCursorQueryUntrusted, drawn.Name, measured[index], drawn.Columns), "")
				}
			}

			return reportGlyphWidths(drawings[len(calibration):], measured[len(calibration):], env.InsideTmux != "")
		},
	}
}

// newTerminalEmphasisCheck reports whether the terminal that paints honours the bold and italic
// escapes, which is what tells a workflow row from an agent row and an agent row from a child.
func newTerminalEmphasisCheck() Check {
	return Check{
		Name:        "terminal emphasis",
		Description: "bold marks a workflow row and italic marks a child",
		Run: func(ctx context.Context, env Env) Result {
			painting, err := findPaintingTerm(ctx, env)
			if err != nil {
				return Result{Status: StatusSkip, Detail: "not checked, " + err.Error()}
			}

			missing, losses, err := findMissingEmphasis(ctx, env, painting)
			if err != nil {
				return warn(err, painting)
			}

			if len(missing) > 0 {
				return Result{
					Status: StatusWarn,
					Detail: painting,
					Err: fmt.Errorf("%w: %s names no %s, so %s",
						ErrEmphasisMissing, painting, joinWords(missing), strings.Join(losses, " and ")),
					Fix: "set your terminal's TERM to an entry that names them, such as xterm-256color",
				}
			}

			return pass(painting + " names both bold and italic")
		},
	}
}

// newTerminalColorCheck reports the colour depth terminfo claims, which is what the palette's
// tones are mixed for.
func newTerminalColorCheck() Check {
	return Check{
		Name:        "terminal color",
		Description: "the palette's tones need " + strconv.Itoa(paletteColors) + " colours",
		Run: func(ctx context.Context, env Env) Result {
			if env.Term == "" {
				return warn(ErrTermUnset, "")
			}

			reported, err := env.ReadTerminfo(ctx, env.Term, "colors")
			if err != nil {
				return warn(fmt.Errorf("%w: %w", ErrTerminfoUnreadable, err), env.Term)
			}

			colors, err := strconv.Atoi(strings.TrimSpace(reported))
			if err != nil {
				return warn(fmt.Errorf("%w: %s reported %q colours", ErrTerminfoUnreadable, env.Term, reported), env.Term)
			}

			detail := fmt.Sprintf("%s reports %d colours", env.Term, colors)
			if env.ColorTerm != "" {
				detail += ", and COLORTERM says " + env.ColorTerm
			}

			if colors < paletteColors {
				return Result{
					Status: StatusWarn,
					Detail: detail,
					Err: fmt.Errorf("%w: %d, where the palette is mixed for %d, so tones collapse into each other",
						ErrColorTooShallow, colors, paletteColors),
					Fix: "set TERM to a 256-colour entry, such as xterm-256color, or tmux-256color inside tmux",
				}
			}

			return pass(detail)
		},
	}
}

func textOf(drawn view.Drawn) string { return drawn.Text }

// reportGlyphWidths is the reading turned into a finding, warning rather than failing because a
// row laid out a column wide is a degraded panel rather than a broken weave.
func reportGlyphWidths(drawings []view.Drawn, measured []int, insideTmux bool) Result {
	caveat := "; measured as the cursor's advance rather than the ink, which are the same only where the terminal's width table and the font agree"
	if insideTmux {
		caveat += "; and answered by tmux from its own grid rather than by the terminal"
	}

	reading := describeWidths(drawings, measured) + caveat

	mismatch := describeMismatch(drawings, measured)
	if mismatch == "" {
		return pass(reading)
	}

	return Result{
		Status: StatusWarn,
		Detail: reading,
		Err:    fmt.Errorf("%w: %s%s", ErrGlyphWidthUnexpected, mismatch, caveat),
		Fix:    "report these widths, since what has to move is the layout's own count rather than anything on this machine",
	}
}

// describeMismatch groups the glyphs the terminal and the layout disagree about, since the text
// mode of the report shows this instead of the detail and a list of fifteen would be cut.
func describeMismatch(drawings []view.Drawn, measured []int) string {
	type disagreement struct {
		drew    int
		counted int
	}

	grouped := map[disagreement]int{}
	for index, drawn := range drawings {
		if measured[index] != drawn.Columns {
			grouped[disagreement{drew: measured[index], counted: drawn.Columns}]++
		}
	}

	pairs := slices.SortedFunc(maps.Keys(grouped), func(a, b disagreement) int { return a.drew - b.drew })

	return strings.Join(xslice.Map(pairs, func(pair disagreement) string {
		return fmt.Sprintf("%s drew in %s where the layout counts them as %d",
			count(grouped[pair], "glyph"), count(pair.drew, "column"), pair.counted)
	}), "; ")
}

// describeWidths groups the glyphs by the width they drew in, since what a person wants is how
// uniform they were rather than fifteen separate numbers.
func describeWidths(drawings []view.Drawn, measured []int) string {
	grouped := map[int][]string{}
	for index, drawn := range drawings {
		grouped[measured[index]] = append(grouped[measured[index]], drawn.Name)
	}

	widths := slices.Sorted(maps.Keys(grouped))

	return strings.Join(xslice.Map(widths, func(width int) string {
		return fmt.Sprintf("%s: %s", count(width, "column"), strings.Join(grouped[width], ", "))
	}), "; ")
}

// count is a number and the thing it counts, plural unless there is one of them.
func count(number int, noun string) string {
	if number == 1 {
		return "1 " + noun
	}

	return strconv.Itoa(number) + " " + noun + "s"
}

// Explainer: lipgloss emits SGR 1 and SGR 3 from termenv's hardcoded BoldSeq and ItalicSeq
// without consulting terminfo, so the panel sends both escapes whatever TERM says and the entry
// TERM names inside tmux gates nothing. What decides whether a person sees italics is the
// terminal tmux paints to, which converts them to standout when its own entry names no sitm.

// findPaintingTerm is the terminfo entry of the terminal that actually paints the panel, which
// inside tmux is tmux's own client rather than the entry tmux advertises to what it runs.
func findPaintingTerm(ctx context.Context, env Env) (string, error) {
	if env.InsideTmux == "" {
		if env.Term == "" {
			return "", ErrTermUnset
		}

		return env.Term, nil
	}

	client, err := env.Output(ctx, "tmux", "display-message", "-p", "#{client_termname}")
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrPaintingTermUnknown, err)
	}

	if strings.TrimSpace(client) == "" {
		return "", fmt.Errorf("%w: nothing is attached to this session", ErrPaintingTermUnknown)
	}

	return strings.TrimSpace(client), nil
}

// findMissingEmphasis reads both escapes out of terminfo and returns the ones the entry does
// not name, alongside what the list loses without each.
func findMissingEmphasis(ctx context.Context, env Env, term string) (missing, losses []string, err error) {
	for _, wanted := range emphasis {
		escape, err := env.ReadTerminfo(ctx, term, wanted.capability)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %w", ErrTerminfoUnreadable, err)
		}

		if escape == "" {
			missing = append(missing, wanted.capability)
			losses = append(losses, wanted.loss)
		}
	}

	return missing, losses, nil
}
