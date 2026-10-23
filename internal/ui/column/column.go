package column

// Narrowest is the narrowest a document is worth rendering beside a list at.
const Narrowest = 40

// DividerCols is the columns the divider between the list and the block beside it takes, a gutter
// on each side of its glyph.
const DividerCols = 3

// Split is how the window is divided while the panel is rendering a block beside its own list: the
// block on one side, and the list, its record and its legend on the other.
type Split struct {
	ColumnWidth  int
	SidebarWidth int
	RowCount     int
	// IsDivided is whether the window is divided at all.
	IsDivided bool
}
