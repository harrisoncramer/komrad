package config

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// maxTagLen bounds a tag in bytes. A glyph runs to a handful even with modifiers on it,
// so anything longer is not the one character the panel can draw.
const maxTagLen = 16

// hexColor matches the colours lipgloss reads, three or six digits behind a hash. Anything
// else renders as no colour at all, so a typo would silently lose the tag's colour.
var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// isValidColor reports whether the panel could draw a colour. The names are a closed set, so
// a colour spelled wrongly is refused here rather than drawn as no colour.
func isValidColor(color string) bool {
	if _, ok := ansiColors[color]; ok {
		return true
	}

	return hexColor.MatchString(color)
}

// validateTags rejects tags the panel could not draw. A glyph goes straight into a
// rendered frame, so whitespace or a control character would break its row, and a glyph
// listed twice could never be stepped past.
func validateTags(tags []Tag) error {
	unprintable := func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }
	seen := make(map[string]bool, len(tags))

	for _, tag := range tags {
		switch {
		case tag.Glyph == "":
			return errors.New("a tag cannot be empty")
		case len(tag.Glyph) > maxTagLen:
			return fmt.Errorf("tag %q is longer than %d bytes, so it is not one glyph", tag.Glyph, maxTagLen)
		case strings.ContainsFunc(tag.Glyph, unprintable):
			return fmt.Errorf("tag %q carries whitespace or control characters", tag.Glyph)
		case seen[tag.Glyph]:
			return fmt.Errorf("tag %q is listed twice", tag.Glyph)
		case tag.Color != "" && !isValidColor(tag.Color):
			return fmt.Errorf("tag %q has colour %q, which is neither one of the sixteen such as \"yellow\" nor a hex colour such as \"#DCA561\"", tag.Glyph, tag.Color)
		}

		seen[tag.Glyph] = true
	}

	return nil
}

// validateLayout rejects a layout tmux could not lay the panes out at. Zero is how an omitted
// size reads, so it falls back to the default rather than being called a mistake.
func validateLayout(layout Layout) error {
	if err := validatePaneSize("layout.sidebar_width", layout.SidebarWidth); err != nil {
		return err
	}
	if err := validatePaneSize("layout.info_height", layout.InfoHeight); err != nil {
		return err
	}
	if err := validatePaneSize("layout.details_height", layout.DetailsHeight); err != nil {
		return err
	}
	if err := validateSide("layout.sidebar_side", layout.SidebarSide, SidebarSideLeft, SidebarSideRight); err != nil {
		return err
	}

	return validateSide("layout.info_side", layout.InfoSide, InfoSideBelow, InfoSideAbove)
}

// validatePaneSize rejects a size that is neither a share of what it divides nor a whole
// number of cells, since tmux is given one or the other and 1.5 columns is neither.
func validatePaneSize(key string, size float64) error {
	switch {
	case size < 0:
		return fmt.Errorf("%s: %v is not a size", key, size)
	case size >= 1 && size != math.Trunc(size):
		return fmt.Errorf("%s: %v is neither a share below one nor a whole number of cells", key, size)
	}

	return nil
}

// validateSide rejects a side that names neither of the two a region can sit on.
func validateSide(key, side string, allowed ...string) error {
	trimmed := strings.TrimSpace(side)
	if trimmed == "" || slices.Contains(allowed, trimmed) {
		return nil
	}

	return fmt.Errorf("%s: %q is not one of %s", key, side, strings.Join(allowed, ", "))
}

// validate rejects the settings the panel could not run on, so a config that cannot work
// says so when it is read rather than when something tries to draw with it.
func (c Config) validate() error {
	if err := validateTags(c.Tags); err != nil {
		return err
	}
	if err := validateLog(c.Log); err != nil {
		return err
	}
	// A negative ceiling would read as zero and take the default, leaving the file saying
	// one thing while the panel does another.
	if c.MaxAgents < 0 {
		return fmt.Errorf("max_agents: %d is not a number of agents", c.MaxAgents)
	}

	return validateLayout(c.Layout)
}

// logLevels are the levels the service understands. One it does not know decodes as debug,
// so an unchecked typo turns the log up instead of saying the word was wrong.
var logLevels = []string{"debug", "info", "warn", "error"}

// validateLog rejects a level the service could not act on.
func validateLog(log Log) error {
	level := strings.TrimSpace(log.Level)
	if level == "" || slices.Contains(logLevels, strings.ToLower(level)) {
		return nil
	}

	return fmt.Errorf("log.level: %q is not one of %s", log.Level, strings.Join(logLevels, ", "))
}
