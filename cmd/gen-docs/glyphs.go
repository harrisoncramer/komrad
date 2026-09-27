package main

import (
	"fmt"

	"komrad/internal/glyph"
	"komrad/lib/xslice"
)

// glyphReference is every character a glyph may be drawn with, which the docs site lists and
// cuts its font down to.
type glyphReference struct {
	NerdFontsVersion string           `json:"nerdFontsVersion"`
	Characters       []glyphCharacter `json:"characters"`
}

type glyphCharacter struct {
	CodePoint string `json:"codePoint"`
	Character string `json:"character"`
	Name      string `json:"name"`
}

func buildGlyphs() glyphReference {
	return glyphReference{
		NerdFontsVersion: glyph.NerdFontsVersion,
		Characters: xslice.Map(glyph.ApprovedSet(), func(c glyph.Character) glyphCharacter {
			return glyphCharacter{
				CodePoint: fmt.Sprintf("U+%04X", c.Rune),
				Character: string(c.Rune),
				Name:      c.Name,
			}
		}),
	}
}
