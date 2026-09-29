package glyph

// Character is one character a glyph may be drawn with, and what it is called.
type Character struct {
	Rune rune
	Name string
}

// named is every character outside the octicons a glyph may be drawn with, none of them one a
// terminal might draw as an emoji.
var named = []Character{
	{Rune: '!', Name: "exclamation mark"},
	{Rune: '·', Name: "middle dot"},
	{Rune: '›', Name: "single right-pointing angle quotation mark"},
	{Rune: '↑', Name: "upwards arrow"},
	{Rune: '→', Name: "rightwards arrow"},
	{Rune: '↓', Name: "downwards arrow"},
	{Rune: '─', Name: "box drawings light horizontal"},
	{Rune: '│', Name: "box drawings light vertical"},
	{Rune: '┃', Name: "box drawings heavy vertical"},
	{Rune: '└', Name: "box drawings light up and right"},
	{Rune: '├', Name: "box drawings light vertical and right"},
	{Rune: '┤', Name: "box drawings light vertical and left"},
	{Rune: '┼', Name: "box drawings light vertical and horizontal"},
	{Rune: '▏', Name: "left one eighth block"},
	{Rune: '▸', Name: "black right-pointing small triangle"},
	{Rune: '▽', Name: "white down-pointing triangle"},
	{Rune: '○', Name: "white circle"},
	{Rune: '●', Name: "black circle"},
	{Rune: '◆', Name: "black diamond"},
	{Rune: '◇', Name: "white diamond"},
	{Rune: '✓', Name: "check mark"},
	{Rune: '✗', Name: "ballot x"},
}

// approved is every character a glyph may be drawn with, by rune.
var approved = func() map[rune]Character {
	all := make(map[rune]Character, len(octicons)+len(named))
	for _, list := range [][]Character{octicons, named} {
		for _, a := range list {
			all[a.Rune] = a
		}
	}

	return all
}()

// ApprovedSet is every character a glyph may be drawn with, the octicons first and then the
// named characters, each list in the order of its code points.
func ApprovedSet() []Character {
	return append(append([]Character{}, octicons...), named...)
}

// IsApproved reports whether a glyph may be drawn with the character.
func IsApproved(r rune) bool {
	_, ok := approved[r]

	return ok
}
