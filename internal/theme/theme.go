// Package theme is the colours the panel may be drawn in, each spelled as a terminal colour
// number or a hex code, and named for the part it plays rather than for how it looks.
package theme

import (
	"fmt"
	"strconv"
	"strings"

	"komrad/internal/fail"
)

// ErrBadColor marks a colour spelled neither as a terminal colour number nor as a hex code.
var ErrBadColor = fail.InvalidArgument("not a terminal colour number from 0 to 255 or a hex code like #1e3a5f")

// Colors spells every colour the panel draws in.
type Colors struct {
	Primary      string
	Secondary    string
	Muted        string
	Success      string
	Warning      string
	Danger       string
	UserMe       string
	UserAgent    string
	UserReviewer string
	UserGithub   string
	Merged       string
	Highlight    string
	Inverse      string
	DiffAdded    Pair
	DiffDeleted  Pair
	Selection    Pair
	SelectionDim Pair
	Match        Pair
}

// Pair is a background with one colour for a light terminal and another for a dark one.
type Pair struct {
	Light string
	Dark  string
}

// namedColor is one colour and the key the config spells it under.
type namedColor struct {
	key   string
	value string
}

// Defaults are the colours the panel draws in when the config sets none.
func Defaults() Colors {
	return Colors{
		Primary:      "4",
		Secondary:    "6",
		Muted:        "8",
		Success:      "10",
		Warning:      "11",
		Danger:       "9",
		UserMe:       "12",
		UserAgent:    "13",
		UserReviewer: "14",
		UserGithub:   "3",
		Merged:       "5",
		Highlight:    "15",
		Inverse:      "0",
		DiffAdded:    Pair{Light: "#dafbe1", Dark: "#1b3823"},
		DiffDeleted:  Pair{Light: "#ffebe9", Dark: "#402222"},
		Selection:    Pair{Light: "#dbeafe", Dark: "#1e3a5f"},
		SelectionDim: Pair{Light: "#e5e7eb", Dark: "#2b3038"},
		Match:        Pair{Light: "#fde68a", Dark: "#7c5c00"},
	}
}

// Check refuses a set holding a colour the panel cannot draw, naming its key.
func Check(colors Colors) error {
	for _, named := range colors.named() {
		if err := checkColor(named.value); err != nil {
			return fmt.Errorf("failed to read %s: %w", named.key, err)
		}
	}

	return nil
}

// IsNumber is whether a colour is spelled as a terminal colour number.
func IsNumber(spelled string) bool {
	return !strings.HasPrefix(spelled, "#")
}

// named lists every colour beside the key the config spells it under.
func (c Colors) named() []namedColor {
	return []namedColor{
		{key: "primary", value: c.Primary},
		{key: "secondary", value: c.Secondary},
		{key: "muted", value: c.Muted},
		{key: "success", value: c.Success},
		{key: "warning", value: c.Warning},
		{key: "danger", value: c.Danger},
		{key: "user_me", value: c.UserMe},
		{key: "user_agent", value: c.UserAgent},
		{key: "user_reviewer", value: c.UserReviewer},
		{key: "user_github", value: c.UserGithub},
		{key: "merged", value: c.Merged},
		{key: "highlight", value: c.Highlight},
		{key: "inverse", value: c.Inverse},
		{key: "diff_added.light", value: c.DiffAdded.Light},
		{key: "diff_added.dark", value: c.DiffAdded.Dark},
		{key: "diff_deleted.light", value: c.DiffDeleted.Light},
		{key: "diff_deleted.dark", value: c.DiffDeleted.Dark},
		{key: "selection.light", value: c.Selection.Light},
		{key: "selection.dark", value: c.Selection.Dark},
		{key: "selection_dim.light", value: c.SelectionDim.Light},
		{key: "selection_dim.dark", value: c.SelectionDim.Dark},
		{key: "match.light", value: c.Match.Light},
		{key: "match.dark", value: c.Match.Dark},
	}
}

// checkColor refuses a colour spelled neither as a terminal colour number nor as a hex code.
func checkColor(spelled string) error {
	if hex, ok := strings.CutPrefix(spelled, "#"); ok {
		if _, err := strconv.ParseUint(hex, 16, 32); err != nil || len(hex) != 6 {
			return fmt.Errorf("%w: %q", ErrBadColor, spelled)
		}

		return nil
	}

	number, err := strconv.Atoi(spelled)
	if err != nil || number < 0 || number > 255 {
		return fmt.Errorf("%w: %q", ErrBadColor, spelled)
	}

	return nil
}
