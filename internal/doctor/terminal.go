package doctor

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"weave/internal/ui/paint/label"
	"weave/internal/view"
	"weave/lib/xslice"
)

// paletteColors is the number of colours the panel's palette is mixed for.
const paletteColors = 256

var (
	errNoTerminal = &ProblemError{
		Code:    "no_terminal",
		Message: "this is not running on a terminal, so nothing could be asked of one",
	}
	errTerminalWouldNotYield = &ProblemError{
		Code:    "terminal_would_not_yield",
		Message: "the terminal would not be put into raw mode, so its answer could not be read back",
	}
	errCursorQueryUnanswered = &ProblemError{
		Code:    "cursor_query_unanswered",
		Message: "the terminal did not answer a cursor position query, so no width was measured",
	}
	errCursorQueryUntrusted = &ProblemError{
		Code:    "cursor_query_untrusted",
		Message: "the terminal misreported the width of a character whose width is not in doubt",
	}
	errGlyphWidthUnexpected = &ProblemError{
		Code:    "glyph_width_unexpected",
		Message: "the terminal advances the cursor a different number of columns for a glyph than the list's layout counts on",
	}
	errTermUnset = &ProblemError{
		Code:    "term_unset",
		Message: "TERM names no terminal, so terminfo has nothing to answer about",
	}
	errPaintingTermUnknown = &ProblemError{
		Code:    "painting_term_unknown",
		Message: "tmux would not name the terminal it paints to",
	}
	errTerminfoUnreadable = &ProblemError{
		Code:    "terminfo_unreadable",
		Message: "terminfo could not be read, so what this terminal can draw is unknown",
	}
	errEmphasisMissing = &ProblemError{
		Code:    "emphasis_missing",
		Message: "terminfo names no escape for the lettering that says what a row is",
	}
	errItalicSubstituted = &ProblemError{
		Code:    "italic_substituted",
		Message: "tmux will not send italics on and draws them reversed instead",
	}
	errDefaultTerminalUnknown = &ProblemError{
		Code:    "default_terminal_unknown",
		Message: "tmux would not say which entry it advertises, so whether it sends italics is unknown",
	}
	errColorTooShallow = &ProblemError{
		Code:    "color_too_shallow",
		Message: "terminfo reports fewer colours than the palette is written for",
	}
)

// calibration is characters of undisputed width, measured alongside the glyphs.
var calibration = []view.Drawn{
	{
		Name:    "latin x",
		Text:    "x",
		Columns: 1,
	},
	{
		Name:    "han character",
		Text:    "漢",
		Columns: 2,
	},
}

// emphasisFault is a terminfo escape the list letters a row with and what is lost without it.
type emphasisFault struct {
	capability string
	loss       string
}

// emphasis is every escape the list letters a row with.
var emphasis = []emphasisFault{
	{capability: "bold", loss: "a workflow row reads as an ordinary agent"},
	{capability: "sitm", loss: "a child reads as an ordinary agent"},
}

// screenEntry and screenEntryPrefix are the entries tmux refuses to send italics on.
const (
	screenEntry       = "screen"
	screenEntryPrefix = "screen-"
)

// newTerminalGlyphCheck measures how many columns the terminal draws each of the list's glyphs in.
func newTerminalGlyphCheck() Check {
	return Check{
		Name:        "terminal glyphs",
		Description: "the list's glyphs are drawn in the columns its layout counts them as",
		Run: func(ctx context.Context, env Env) Result {
			if env.MeasureColumns == nil {
				return skip("nothing is attached to a terminal to ask")
			}

			drawings := append(slices.Clone(calibration), view.Drawings()...)

			measured, err := env.MeasureColumns(ctx, xslice.Map(drawings, textOf))
			if err != nil {
				if codeOf(err) == errNoTerminal.Code {
					return skip(err.Error())
				}

				return warn(err, "")
			}

			if len(measured) != len(drawings) {
				return warn(fmt.Errorf("%w: asked about %d, heard back about %d",
					errCursorQueryUnanswered, len(drawings), len(measured)), "")
			}

			for index, drawn := range calibration {
				if measured[index] != drawn.Columns {
					return warn(fmt.Errorf("%w: %s drew in %d columns rather than %d, so the rest of the reading is not worth quoting",
						errCursorQueryUntrusted, drawn.Name, measured[index], drawn.Columns), "")
				}
			}

			return reportGlyphWidths(drawings[len(calibration):], measured[len(calibration):], env.InsideTmux != "")
		},
	}
}

// newTerminalEmphasisCheck reports whether the terminal that paints honours the bold and italic
// escapes.
func newTerminalEmphasisCheck() Check {
	return Check{
		Name:        "terminal emphasis",
		Description: "bold marks a workflow row and italic marks a child",
		Run: func(ctx context.Context, env Env) Result {
			painting, err := findPaintingTerm(ctx, env)
			if err != nil {
				return skip(err.Error())
			}

			missing, err := findMissingEmphasis(ctx, env, painting)
			if err != nil {
				return warn(err, painting)
			}

			substituted, err := doesTmuxSubstituteItalics(ctx, env)
			if err != nil {
				return skip(err.Error())
			}

			faults, fixes := describeEmphasisFaults(painting, missing, substituted)
			if len(faults) > 0 {
				problem := errItalicSubstituted
				if len(missing) > 0 {
					problem = errEmphasisMissing
				}

				return Result{
					Status: StatusWarn,
					Detail: painting,
					Err:    fmt.Errorf("%w: %s", problem, strings.Join(faults, "; and ")),
					Fix:    strings.Join(fixes, "; "),
				}
			}

			return pass(painting + " names both bold and italic")
		},
	}
}

// newTerminalColorCheck reports the colour depth terminfo claims.
func newTerminalColorCheck() Check {
	return Check{
		Name:        "terminal color",
		Description: "the palette's tones need " + strconv.Itoa(paletteColors) + " colours",
		Run: func(ctx context.Context, env Env) Result {
			if env.Term == "" {
				return warn(errTermUnset, "")
			}

			reported, err := env.ReadTerminfo(ctx, env.Term, "colors")
			if err != nil {
				return warn(fmt.Errorf("%w: %w", errTerminfoUnreadable, err), env.Term)
			}

			colors, err := strconv.Atoi(strings.TrimSpace(reported))
			if err != nil {
				return warn(fmt.Errorf("%w: %s reported %q colours", errTerminfoUnreadable, env.Term, reported), env.Term)
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
						errColorTooShallow, colors, paletteColors),
					Fix: "set TERM to a 256-colour entry, such as xterm-256color, or tmux-256color inside tmux",
				}
			}

			return pass(detail)
		},
	}
}

func textOf(drawn view.Drawn) string {
	return drawn.Text
}

// reportGlyphWidths is the reading turned into a finding.
func reportGlyphWidths(drawings []view.Drawn, measured []int, insideTmux bool) Result {
	caveat := ""
	if insideTmux {
		caveat = " (as tmux counts them)"
	}

	mismatch := describeMismatch(drawings, measured)
	if mismatch == "" {
		return pass(label.Counted(len(drawings), "glyph") + " drawn at the widths the layout counts on" + caveat)
	}

	return Result{
		Status: StatusWarn,
		Detail: describeWidths(drawings, measured) + caveat,
		Err:    fmt.Errorf("%w: %s%s", errGlyphWidthUnexpected, mismatch, caveat),
		Fix:    "report these widths, since what has to move is the layout's own count rather than anything on this machine",
	}
}

// describeMismatch groups the glyphs the terminal and the layout disagree about.
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
			label.Counted(grouped[pair], "glyph"), label.Counted(pair.drew, "column"), pair.counted)
	}), "; ")
}

// describeWidths groups the glyphs by the width they drew in.
func describeWidths(drawings []view.Drawn, measured []int) string {
	grouped := map[int][]string{}
	for index, drawn := range drawings {
		grouped[measured[index]] = append(grouped[measured[index]], drawn.Name)
	}

	widths := slices.Sorted(maps.Keys(grouped))

	return strings.Join(xslice.Map(widths, func(width int) string {
		return fmt.Sprintf("%s: %s", label.Counted(width, "column"), strings.Join(grouped[width], ", "))
	}), "; ")
}

// findPaintingTerm is the terminfo entry of the terminal that paints the panel, which inside
// tmux is tmux's own client.
func findPaintingTerm(ctx context.Context, env Env) (string, error) {
	if env.InsideTmux == "" {
		if env.Term == "" {
			return "", errTermUnset
		}

		return env.Term, nil
	}

	client, err := env.Output(ctx, "tmux", "display-message", "-p", "#{client_termname}")
	if err != nil {
		return "", fmt.Errorf("%w: %w", errPaintingTermUnknown, err)
	}

	if strings.TrimSpace(client) == "" {
		return "", fmt.Errorf("%w: nothing is attached to this session", errPaintingTermUnknown)
	}

	return strings.TrimSpace(client), nil
}

// reversedChild is what becomes of a child row wherever tmux stands in the way.
const reversedChild = "a child row is drawn reversed rather than italic"

// describeEmphasisFaults is everything wrong with a row's lettering and what to do about each.
func describeEmphasisFaults(painting string, missing []emphasisFault, substituted bool) (faults, fixes []string) {
	if len(missing) > 0 {
		faults = append(faults, fmt.Sprintf("%s names no %s, so %s",
			painting, joinWords(xslice.Map(missing, capabilityOf)), strings.Join(xslice.Map(missing, lossOf), " and ")))
		fixes = append(fixes, "set your terminal's TERM to an entry that names them, such as xterm-256color")
	}

	if substituted && !slices.ContainsFunc(missing, isItalic) {
		faults = append(faults, "tmux sends standout in place of italics, so "+reversedChild)
		fixes = append(fixes, "set tmux's default-terminal to tmux-256color rather than a screen entry")
	}

	return faults, fixes
}

// findMissingEmphasis reads both escapes out of terminfo and returns the ones the entry does
// not name.
func findMissingEmphasis(ctx context.Context, env Env, term string) ([]emphasisFault, error) {
	var missing []emphasisFault

	for _, wanted := range emphasis {
		escape, err := env.ReadTerminfo(ctx, term, wanted.capability)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errTerminfoUnreadable, err)
		}

		if escape != "" {
			continue
		}

		if isItalic(wanted) && env.InsideTmux != "" {
			wanted.loss = reversedChild
		}

		missing = append(missing, wanted)
	}

	return missing, nil
}

func capabilityOf(fault emphasisFault) string {
	return fault.capability
}

func lossOf(fault emphasisFault) string {
	return fault.loss
}

func isItalic(fault emphasisFault) bool {
	return fault.capability == "sitm"
}

// doesTmuxSubstituteItalics reports whether tmux's own default-terminal is a screen entry.
func doesTmuxSubstituteItalics(ctx context.Context, env Env) (bool, error) {
	if env.InsideTmux == "" {
		return false, nil
	}

	entry, err := env.Output(ctx, "tmux", "show-option", "-gqv", "default-terminal")
	if err != nil {
		return false, fmt.Errorf("%w: %w", errDefaultTerminalUnknown, err)
	}

	entry = strings.TrimSpace(entry)

	return entry == screenEntry || strings.HasPrefix(entry, screenEntryPrefix), nil
}
