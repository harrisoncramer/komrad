package config

import (
	"fmt"

	"komrad/internal/theme"
)

// Palette is the colour of each part the panel draws.
type Palette struct {
	// Focus, titles and whatever holds the keys.
	Primary string `mapstructure:"primary"`
	// A second accent for what is set apart from the primary chrome, such as diff hunk headers and
	// inline code.
	Secondary string `mapstructure:"secondary"`
	// Text that matters less than what is around it.
	Muted string `mapstructure:"muted"`
	// Something finished or approved.
	Success string `mapstructure:"success"`
	// Something that needs your attention.
	Warning string `mapstructure:"warning"`
	// Something failed or is gone.
	Danger string `mapstructure:"danger"`
	// What you wrote.
	UserMe string `mapstructure:"user_me"`
	// What an agent wrote.
	UserAgent string `mapstructure:"user_agent"`
	// What a reviewer agent wrote.
	UserReviewer string `mapstructure:"user_reviewer"`
	// What came from GitHub.
	UserGithub string `mapstructure:"user_github"`
	// A merged pull request.
	Merged string `mapstructure:"merged"`
	// What is picked out for you to act on: a search match, a jump letter, the visual mode bar.
	Highlight string `mapstructure:"highlight"`
	// Text drawn on a filled bar.
	Inverse string `mapstructure:"inverse"`
	// The background of a line a diff adds.
	DiffAdded Shade `mapstructure:"diff_added"`
	// The background of a line a diff deletes.
	DiffDeleted Shade `mapstructure:"diff_deleted"`
	// The background of the row under the cursor or taken into a selection.
	Selection Shade `mapstructure:"selection"`
	// The background of the cursor's row while the keys are elsewhere.
	SelectionDim Shade `mapstructure:"selection_dim"`
	// The background of a search match in a diff.
	Match Shade `mapstructure:"match"`
}

// Shade is a background with one colour for a light terminal and one for a dark terminal.
type Shade struct {
	// The colour on a light terminal.
	Light string `mapstructure:"light"`
	// The colour on a dark terminal.
	Dark string `mapstructure:"dark"`
}

// DefaultPalette is the palette used when the config omits it, read from the theme package.
func DefaultPalette() Palette {
	colors := theme.Defaults()

	return Palette{
		Primary:      colors.Primary,
		Secondary:    colors.Secondary,
		Muted:        colors.Muted,
		Success:      colors.Success,
		Warning:      colors.Warning,
		Danger:       colors.Danger,
		UserMe:       colors.UserMe,
		UserAgent:    colors.UserAgent,
		UserReviewer: colors.UserReviewer,
		UserGithub:   colors.UserGithub,
		Merged:       colors.Merged,
		Highlight:    colors.Highlight,
		Inverse:      colors.Inverse,
		DiffAdded:    Shade(colors.DiffAdded),
		DiffDeleted:  Shade(colors.DiffDeleted),
		Selection:    Shade(colors.Selection),
		SelectionDim: Shade(colors.SelectionDim),
		Match:        Shade(colors.Match),
	}
}

// Colors is the palette spelled the way the theme package takes it.
func (p Palette) Colors() theme.Colors {
	return theme.Colors{
		Primary:      p.Primary,
		Secondary:    p.Secondary,
		Muted:        p.Muted,
		Success:      p.Success,
		Warning:      p.Warning,
		Danger:       p.Danger,
		UserMe:       p.UserMe,
		UserAgent:    p.UserAgent,
		UserReviewer: p.UserReviewer,
		UserGithub:   p.UserGithub,
		Merged:       p.Merged,
		Highlight:    p.Highlight,
		Inverse:      p.Inverse,
		DiffAdded:    theme.Pair(p.DiffAdded),
		DiffDeleted:  theme.Pair(p.DiffDeleted),
		Selection:    theme.Pair(p.Selection),
		SelectionDim: theme.Pair(p.SelectionDim),
		Match:        theme.Pair(p.Match),
	}
}

// validatePalette refuses a colour the panel cannot draw, naming the key it was written under.
func validatePalette(palette Palette) error {
	if err := theme.Check(palette.Colors()); err != nil {
		return fmt.Errorf("palette: %w", err)
	}

	return nil
}
