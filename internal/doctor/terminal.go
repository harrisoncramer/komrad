package doctor

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"weave/internal/view"
	"weave/lib/xslice"
)

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

// calibration is a character of undisputed width, measured alongside the glyphs so a terminal
// answering nonsense is caught rather than believed.
var calibration = []Drawn{
	{Name: "latin x", Text: "x", Columns: 1},
	{Name: "han character", Text: "漢", Columns: 2},
}

// Drawn is one thing to draw and how many columns the panel's layout counts it as.
type Drawn struct {
	Name    string
	Text    string
	Columns int
}

// paletteColors is the number of colours the panel's palette is written against.
const paletteColors = 256

// newTerminalGlyphCheck measures what the terminal draws each of the list's glyphs in, which is
// the one thing here that is measured rather than inferred.
func newTerminalGlyphCheck() Check {
	return Check{
		Name:        "terminal glyphs",
		Description: "the list's glyphs are drawn in the columns its layout counts them as",
		Run: func(ctx context.Context, env Env) Result {
			if env.MeasureColumns == nil {
				return Result{
					Status: StatusSkip,
					Detail: "not checked, nothing is attached to a terminal to ask",
				}
			}

			drawings := append(append([]Drawn{}, calibration...), panelGlyphs()...)
			samples := xslice.Map(drawings, func(drawn Drawn) string { return drawn.Text })

			measured, err := env.MeasureColumns(ctx, samples)
			if err != nil {
				return skipOrWarn(err)
			}

			if len(measured) != len(drawings) {
				return warn(
					fmt.Errorf("%w: asked about %d, heard back about %d", ErrCursorQueryUnanswered, len(drawings), len(measured)),
					"",
				)
			}

			for index, drawn := range calibration {
				if measured[index] != drawn.Columns {
					return warn(
						fmt.Errorf("%w: %s drew in %d columns rather than %d, so the rest of the reading is not worth reporting",
							ErrCursorQueryUntrusted, drawn.Name, measured[index], drawn.Columns),
						"",
					)
				}
			}

			return reportGlyphWidths(drawings[len(calibration):], measured[len(calibration):], env.InsideTmux != "")
		},
	}
}

// skipOrWarn keeps a terminal that was never there apart from one that would not answer, since
// only the second is worth a person's attention.
func skipOrWarn(err error) Result {
	if codeOf(err) == ErrNoTerminal.Code {
		return Result{Status: StatusSkip, Detail: "not checked, " + err.Error()}
	}

	return warn(err, "")
}

// panelGlyphs is the list's own glyph inventory, each named and carrying the width the layout
// assumes of it.
func panelGlyphs() []Drawn {
	return xslice.Map(view.Drawings(), func(drawing view.Drawn) Drawn {
		return Drawn{Name: drawing.Name, Text: drawing.Text, Columns: drawing.Columns}
	})
}

func reportGlyphWidths(drawings []Drawn, measured []int, insideTmux bool) Result {
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

// describeWidths groups the glyphs by the width they drew in, since the reading a person wants
// is how uniform they were rather than fifteen separate numbers.
func describeWidths(drawings []Drawn, measured []int) string {
	grouped := map[int][]string{}
	for index, drawn := range drawings {
		grouped[measured[index]] = append(grouped[measured[index]], drawn.Name)
	}

	widths := make([]int, 0, len(grouped))
	for width := range grouped {
		widths = append(widths, width)
	}
	slices.Sort(widths)

	parts := xslice.Map(widths, func(width int) string {
		return fmt.Sprintf("%d columns: %s", width, strings.Join(grouped[width], ", "))
	})

	return strings.Join(parts, "; ")
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

			missing, err := missingEmphasis(ctx, env)
			if err != nil {
				return warn(err, env.Term)
			}

			if len(missing) > 0 {
				return Result{
					Status: StatusWarn,
					Detail: env.Term,
					Err: fmt.Errorf("%w: %s names no %s, so %s",
						ErrEmphasisMissing, env.Term, joinWords(missing), describeEmphasisLoss(missing)),
					Fix: "set TERM to an entry that names them, such as xterm-256color, or tmux-256color inside tmux",
				}
			}

			return pass(env.Term + " names both bold and italic")
		},
	}
}

// emphasisCapabilities are the terminfo names of the two escapes, paired with what the list
// spends each one on.
var emphasisCapabilities = []struct {
	capability string
	spentOn    string
}{
	{capability: "bold", spentOn: "a workflow row reads as an ordinary agent"},
	{capability: "sitm", spentOn: "a child reads as an ordinary agent"},
}

func missingEmphasis(ctx context.Context, env Env) ([]string, error) {
	var missing []string
	for _, wanted := range emphasisCapabilities {
		escape, err := env.ReadTerminfo(ctx, wanted.capability)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrTerminfoUnreadable, err)
		}

		if escape == "" {
			missing = append(missing, wanted.capability)
		}
	}

	return missing, nil
}

func describeEmphasisLoss(missing []string) string {
	losses := make([]string, 0, len(missing))
	for _, wanted := range emphasisCapabilities {
		if slices.Contains(missing, wanted.capability) {
			losses = append(losses, wanted.spentOn)
		}
	}

	return strings.Join(losses, " and ")
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
