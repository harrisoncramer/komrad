package view

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
)

// RowKind is what one line of the agents list stands for, an agent leading so a row that names
// no kind is the ordinary one.
type RowKind int

const (
	KindAgent RowKind = iota
	KindWorkflow
	KindChild
	KindResult
	KindHit
)

// Process is how much of a process stands behind a row.
type Process int

const (
	ProcessAlive Process = iota
	ProcessStopped
	ProcessNone
)

// Status is what an agent is doing this second.
type Status int

const (
	StatusIdle Status = iota
	StatusThinking
	StatusWaiting
	StatusBroken
)

// Repo is where an agent's own checkout stands.
type Repo int

const (
	RepoClean Repo = iota
	RepoRebasing
	RepoGone
	// RepoEnd is one past the last state, so a caller walking them all gets a bound that moves
	// when a state is added.
	RepoEnd
)

// Forge is where the pull request opened from an agent's branch stands.
type Forge int

const (
	ForgeUnread Forge = iota
	ForgeNone
	ForgeOpen
	ForgeApproved
	ForgeApprovedWaiting
	ForgeChangesRequested
	ForgeChecksRunning
	ForgeDraft
	ForgeFailing
	ForgeConflicting
	ForgeMerged
	ForgeClosed
	// ForgeEnd is one past the last state, so a caller walking them all gets a bound that moves
	// when a state is added.
	ForgeEnd
)

// Span is a stretch of a name a search matched, in bytes of that name.
type Span = paint.Span

// Caret is how a list draws the arrow on its focused row.
type Caret int

const (
	// CaretDrawn is the arrow on the row, in the accent with the name.
	CaretDrawn Caret = iota
	// CaretBlinkedOut is the arrow in the hidden half of its blink, the name still in the accent.
	CaretBlinkedOut
	// CaretAbsent is no arrow and no accent, the row only kept on screen.
	CaretAbsent
)

// Snapshot is everything the agents list draws itself from.
type Snapshot struct {
	Rows []Row
	// Caret is how the focused row's arrow stands this frame.
	Caret Caret
	// BlinkedOut says the blink is on its off beat this frame.
	BlinkedOut bool
	// Focused is the key of the row the keyboard is standing on, naming it rather than counting
	// to it so a list re-sorted after it was gathered still focuses the same row.
	Focused string
	// Pane is how many columns the list is drawn in, from which the view works out what is left
	// for a name, leaving everything whole when it is unset.
	Pane int
	// Columns is the gutter this screen draws, and nil is the agents list's own.
	Columns Columns
	// Glyphs is the character each glyph is drawn with.
	Glyphs glyph.Set
}

// columns is the gutter the snapshot draws, the agents list's own where none was named.
func (s Snapshot) columns() Columns {
	if s.Columns != nil {
		return s.Columns
	}

	return WorkColumns()
}

// Comments is how one side's comments on an agent's work stand.
type Comments struct {
	Open     int
	Resolved int
	Unread   bool
	External int
}

// Row is one line of the agents list as the panel found it.
type Row struct {
	// Key is what the row answers to.
	Key  string
	Kind RowKind
	Lead string
	Name string
	// Matches are the stretches of the name a search picked out.
	Matches []Span
	Status  Status
	// Process says whether anything ever stood behind the row, which the bar leaves blank where
	// nothing did and the weight greys once it has gone.
	Process     Process
	Repo        Repo
	Forge       Forge
	LoaderFrame string
	// ReviewerComments counts the comments reviewer agents left, nil while there are none to show.
	ReviewerComments *Comments
	// PersonComments counts the comments the person left, nil while there are none to show.
	PersonComments *Comments
	// Cells fill the list's text columns, indexed by column position, and are ignored under a
	// glyph column and past the last column.
	Cells []Cell
	// Selected says the row has been taken into a selection several rows wide.
	Selected bool
	// Archived says the row has been put away, which greys its name.
	Archived bool
	// Hidden says a person took the row out of the sidebar, drawn as a mark after its name.
	Hidden bool
	Held   bool
	// HeldWorking says a paused agent is working anyway, which the hold did not stop.
	HeldWorking bool
	UnreadMail  bool
	FailedRun   bool
	// Documents says the agent has written something for a person to read.
	Documents bool
	Scheduled bool
	// HiddenCount is how many rows this one stands in for while they are out of sight.
	HiddenCount int
}
