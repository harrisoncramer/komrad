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
		Message: "a glyph is drawn in a different number of columns than the list's layout counts it as",
	}
	ErrTermUnset = &ProblemError{
		Code:    "term_unset",
		Message: "TERM names no terminal, so terminfo has nothing to answer about",
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

// newTerminalEmphasisCheck reports whether terminfo names the bold and italic escapes, which is
// what tells a workflow row from an agent row and an agent row from a child.
func newTerminalEmphasisCheck() Check {
	return Check{
		Name:        "terminal emphasis",
		Description: "bold marks a workflow row and italic marks a child",
		Run: func(ctx context.Context, env Env) Result {
			if env.Term == "" {
				return warn(ErrTermUnset, "")
			}

			missing, losses, err := findMissingEmphasis(ctx, env)
			if err != nil {
				return warn(err, env.Term)
			}

			if len(missing) > 0 {
				return Result{
					Status: StatusWarn,
					Detail: env.Term,
					Err: fmt.Errorf("%w: %s names no %s, so %s",
						ErrEmphasisMissing, env.Term, joinWords(missing), strings.Join(losses, " and ")),
					Fix: "set TERM to an entry that names them, such as xterm-256color, or tmux-256color inside tmux",
				}
			}

			return pass(env.Term + " names both bold and italic")
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

			reported, err := env.ReadTerminfo(ctx, "colors")
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
// font the terminal lacks degrades the panel without breaking weave.
func reportGlyphWidths(drawings []view.Drawn, measured []int, insideTmux bool) Result {
	var wrong []string
	for index, drawn := range drawings {
		if measured[index] != drawn.Columns {
			wrong = append(wrong, fmt.Sprintf("%s drew in %d, counted as %d", drawn.Name, measured[index], drawn.Columns))
		}
	}

	reading := describeWidths(drawings, measured)
	if insideTmux {
		reading += "; measured through tmux, which answers the query from its own grid rather than the terminal's"
	}

	if len(wrong) > 0 {
		return Result{
			Status: StatusWarn,
			Detail: reading,
			Err:    fmt.Errorf("%w: %s", ErrGlyphWidthUnexpected, strings.Join(wrong, ", ")),
			Fix:    "install a Nerd Font and set your terminal to it, or report the widths above so the layout can be corrected",
		}
	}

	return pass(reading)
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
		return fmt.Sprintf("%d columns: %s", width, strings.Join(grouped[width], ", "))
	}), "; ")
}

// findMissingEmphasis reads both escapes out of terminfo and returns the ones the entry does
// not name, alongside what the list loses without each.
func findMissingEmphasis(ctx context.Context, env Env) (missing, losses []string, err error) {
	for _, wanted := range emphasis {
		escape, err := env.ReadTerminfo(ctx, wanted.capability)
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
