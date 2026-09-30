// Package glyph is every character the panel draws: the icons a person may choose in the
// [glyphs] section of the config, and the chrome the layout is drawn with, which is fixed.
package glyph

import (
	"fmt"
	"slices"

	"github.com/charmbracelet/x/ansi"
)

//go:generate go run gen/main.go

// Columns is how many columns every glyph takes.
const Columns = 1

// Glyph names an icon the panel draws, spelled out by a Set.
type Glyph int

const (
	None Glyph = iota
	Worktree
	WorktreeGone
	Open
	ChangesRequested
	Draft
	Approved
	Merged
	Closed
	Failing
	Conflicting
	FailedScript
	Mail
	Paused
	Hidden
	Schedule
	Document
	ReviewerComments
	PersonComments
	Unread
	Workflow
	Archive
	Search
	Filter
	ThreadUnread
	ThreadRead
	ThreadSettled
	ThreadUnsent
	FileViewed
	Delivered
	Held
	MessageError
	MessageWarning
	MessageSuccess
	MessageInfo
	ScheduleEnabled
	ScheduleDisabled
	Engine
	// count stands one past the last glyph.
	count
)

// defined is each glyph's key in the config and the character it draws unless a person chose
// another.
var defined = [count]struct {
	key      string
	fallback string
}{
	Worktree:         {"row.worktree", ""},
	WorktreeGone:     {"row.worktree_gone", ""},
	Open:             {"pull_request.open", ""},
	ChangesRequested: {"pull_request.changes_requested", ""},
	Draft:            {"pull_request.draft", ""},
	Approved:         {"pull_request.approved", ""},
	Merged:           {"pull_request.merged", ""},
	Closed:           {"pull_request.closed", ""},
	Failing:          {"pull_request.failing", ""},
	Conflicting:      {"pull_request.conflicting", ""},
	FailedScript:     {"marks.failed_script", ""},
	Mail:             {"marks.mail", ""},
	Paused:           {"marks.paused", ""},
	Hidden:           {"marks.hidden", ""},
	Schedule:         {"marks.schedule", ""},
	Document:         {"marks.document", ""},
	ReviewerComments: {"marks.reviewer_comments", ""},
	PersonComments:   {"marks.person_comments", ""},
	Unread:           {"marks.unread", "·"},
	Workflow:         {"lists.workflow", ""},
	Archive:          {"lists.archive", ""},
	Search:           {"lists.search", ""},
	Filter:           {"lists.filter", "▽"},
	ThreadUnread:     {"review.unread", "●"},
	ThreadRead:       {"review.read", "○"},
	ThreadSettled:    {"review.settled", "✓"},
	ThreadUnsent:     {"review.unsent", "◇"},
	FileViewed:       {"review.viewed", "✓"},
	Delivered:        {"review.delivered", "✓"},
	Held:             {"review.held", "◇"},
	MessageError:     {"messages.error", "✗"},
	MessageWarning:   {"messages.warning", "!"},
	MessageSuccess:   {"messages.success", "✓"},
	MessageInfo:      {"messages.info", "·"},
	ScheduleEnabled:  {"schedules.enabled", "◆"},
	ScheduleDisabled: {"schedules.disabled", "◇"},
	Engine:           {"lists.engine", "\uf444"},
}

// All is every glyph a Set spells, in the order they are declared.
func All() []Glyph {
	glyphs := make([]Glyph, 0, count-1)
	for g := None + 1; g < count; g++ {
		glyphs = append(glyphs, g)
	}

	return glyphs
}

// Key is where the glyph sits under [glyphs] in the config, its group and its name.
func (g Glyph) Key() string {
	if g <= None || g >= count {
		return ""
	}

	return defined[g].key
}

// ByKey is the glyph the config names by key, and false when no glyph sits there.
func ByKey(key string) (Glyph, bool) {
	for _, g := range All() {
		if defined[g].key == key {
			return g, true
		}
	}

	return None, false
}

// Set is the character each glyph draws, where the zero value draws the defaults.
type Set struct {
	spelled [count]string
}

// Defaults is the set a person who chose nothing draws with.
func Defaults() Set {
	return Set{}
}

// Load is the defaults with the given glyphs drawn as the characters given, refusing a character
// outside the approved set or not one column wide.
func Load(overrides map[Glyph]string) (Set, error) {
	var set Set

	keys := make([]Glyph, 0, len(overrides))
	for g := range overrides {
		keys = append(keys, g)
	}
	slices.Sort(keys)

	for _, g := range keys {
		if g <= None || g >= count {
			return Set{}, fmt.Errorf("glyph %d is not one the panel draws", g)
		}

		value := overrides[g]
		if err := Check(value); err != nil {
			return Set{}, fmt.Errorf("failed to draw %s: %w", g.Key(), err)
		}

		set.spelled[g] = value
	}

	return set, nil
}

// Spell is the character the glyph draws.
func (s Set) Spell(g Glyph) string {
	if g <= None || g >= count {
		return ""
	}
	if spelled := s.spelled[g]; spelled != "" {
		return spelled
	}

	return defined[g].fallback
}

// Check refuses a value that is not exactly one approved character a single column wide.
func Check(value string) error {
	runes := []rune(value)
	if len(runes) != 1 {
		return fmt.Errorf("%q is not a single character", value)
	}
	if !IsApproved(runes[0]) {
		return fmt.Errorf("U+%04X is not an approved glyph", runes[0])
	}
	if width := ansi.StringWidth(value); width != Columns {
		return fmt.Errorf("U+%04X is %d columns wide rather than %d", runes[0], width, Columns)
	}

	return nil
}
