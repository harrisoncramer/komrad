package doctor

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"komrad/internal/glyph"
	"komrad/lib/xslice"
)

// recommendedFont is the font the panel's glyphs are drawn and checked in.
const recommendedFont = "JetBrainsMono Nerd Font"

var (
	errFontsUnlisted = &ProblemError{
		Code:    "fonts_unlisted",
		Message: "the fonts installed on this machine could not be listed",
	}
	errNerdFontMissing = &ProblemError{
		Code:    "nerd_font_missing",
		Message: "no Nerd Font is installed, so the panel's icons are drawn as empty boxes",
	}
	errGlyphCollision = &ProblemError{
		Code:    "glyph_collision",
		Message: "two glyphs drawn in the same sidebar row are the same character, so a row cannot say which it means",
	}
)

// collidingGroups are the groups whose glyphs share a row of the sidebar.
var collidingGroups = []string{"row", "pull_request", "marks"}

// configuredGlyphs is the set the config chooses, or the defaults when it cannot be read.
func configuredGlyphs(env Env) glyph.Set {
	if env.LoadConfig == nil {
		return glyph.Defaults()
	}

	cfg, err := env.LoadConfig()
	if err != nil {
		return glyph.Defaults()
	}

	set, err := cfg.Glyphs.Set()
	if err != nil {
		return glyph.Defaults()
	}

	return set
}

// newGlyphFontCheck reports whether a Nerd Font is installed to draw the panel's icons.
func newGlyphFontCheck() Check {
	return Check{
		Name:        "glyph font",
		Description: "a Nerd Font is installed to draw the panel's icons",
		Run: func(ctx context.Context, env Env) Result {
			if env.ListFonts == nil {
				return skip("nothing was given to list the fonts with")
			}

			fonts, err := env.ListFonts(ctx)
			if err != nil {
				return warn(fmt.Errorf("%w: %w", errFontsUnlisted, err), "")
			}

			found, recommended := findNerdFont(fonts)
			switch {
			case recommended:
				return pass(recommendedFont + " is installed")
			case found != "":
				return pass(found + " is installed, though the glyphs are checked against " + recommendedFont)
			default:
				return Result{
					Status: StatusWarn,
					Err:    errNerdFontMissing,
					Fix:    "install " + recommendedFont + " from nerdfonts.com and set it as your terminal's font",
				}
			}
		},
	}
}

// newGlyphCollisionCheck reports two glyphs in one sidebar row configured as one character.
func newGlyphCollisionCheck() Check {
	return Check{
		Name:        "glyph collisions",
		Description: "the glyphs sharing a sidebar row are each a character of their own",
		Needs:       []string{"config"},
		Run: func(_ context.Context, env Env) Result {
			set := configuredGlyphs(env)

			keysBySpelling := map[string][]string{}
			var spellings []string
			for _, g := range glyph.All() {
				group, _, _ := strings.Cut(g.Key(), ".")
				if !slices.Contains(collidingGroups, group) {
					continue
				}

				spelled := set.Spell(g)
				if _, seen := keysBySpelling[spelled]; !seen {
					spellings = append(spellings, spelled)
				}
				keysBySpelling[spelled] = append(keysBySpelling[spelled], "glyphs."+g.Key())
			}

			var collisions []string
			for _, spelled := range spellings {
				if keys := keysBySpelling[spelled]; len(keys) > 1 {
					collisions = append(collisions, fmt.Sprintf("%s all draw U+%04X", strings.Join(keys, ", "), []rune(spelled)[0]))
				}
			}

			if len(collisions) > 0 {
				return Result{
					Status: StatusWarn,
					Err:    fmt.Errorf("%w: %s", errGlyphCollision, strings.Join(collisions, "; ")),
					Fix:    "give each of them a character of its own under [glyphs]",
				}
			}

			return pass("no two glyphs in a sidebar row are the same character")
		},
	}
}

// findNerdFont is the first Nerd Font among the fonts, and whether the recommended one is there.
func findNerdFont(fonts []string) (string, bool) {
	var found string
	for _, font := range fonts {
		folded := foldFontName(font)
		if !strings.Contains(folded, "nerdfont") {
			continue
		}
		if strings.Contains(folded, "jetbrainsmono") {
			return font, true
		}
		if found == "" {
			found = font
		}
	}

	return found, false
}

// foldFontName is a font's name or file name lowered, with its spaces, dashes and underscores
// taken out.
func foldFontName(name string) string {
	return strings.NewReplacer(" ", "", "-", "", "_", "").Replace(strings.ToLower(name))
}

// listFonts is the fonts installed on this machine, as family names where fontconfig is
// installed and as the file names in the macOS font folders where it is not.
func listFonts(ctx context.Context) ([]string, error) {
	if _, err := exec.LookPath("fc-list"); err == nil || runtime.GOOS != "darwin" {
		out, err := runOutput(ctx, "fc-list", ":", "family")
		if err != nil {
			return nil, err
		}

		return strings.Split(strings.TrimSpace(out), "\n"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	var fonts []string
	for _, dir := range []string{filepath.Join(home, "Library", "Fonts"), "/Library/Fonts", "/System/Library/Fonts"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		fonts = append(fonts, xslice.Map(entries, fs.DirEntry.Name)...)
	}

	return fonts, nil
}
