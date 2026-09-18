package view

// RowKind is what one line of the agents list stands for, an agent leading so a row that names
// no kind is the ordinary one.
type RowKind int

const (
	KindAgent RowKind = iota
	KindWorkflow
	KindChild
	KindResult
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

// Review is where the local review of an agent's work stands.
type Review int

const (
	ReviewNone Review = iota
	ReviewOpen
	ReviewApproved
	ReviewChangesRequested
	ReviewCommented
	// ReviewEnd is one past the last state, so a caller walking them all gets a bound that moves
	// when a state is added.
	ReviewEnd
)

// Forge is where the pull request opened from an agent's branch stands.
type Forge int

const (
	ForgeNone Forge = iota
	ForgeOpen
	ForgeApproved
	ForgeFailing
	ForgeMerged
)

// Span is a stretch of a name a search matched, in bytes of that name.
type Span struct {
	Start int
	End   int
}

// Snapshot is everything the agents list draws itself from.
type Snapshot struct {
	Rows []Row
	// CaretHidden says the blink has the focus caret out of sight this frame, which the caller
	// knows and the view would otherwise have to invent.
	CaretHidden bool
	// Focused is the key of the row the keyboard is standing on, naming it rather than counting
	// to it so a list re-sorted after it was gathered still focuses the same row.
	Focused string
	// Pane is how many columns the list is drawn in, from which the view works out which columns
	// fit and what is left for a name, leaving everything whole when it is unset.
	Pane int
	// Dropped are the columns this screen does not draw at all whatever the pane, naming them the
	// other way round from Columns so a screen that drops none says nothing.
	Dropped Columns
}

// Row is one line of the agents list as the panel found it.
type Row struct {
	// Key is what the row answers to, which is how the snapshot names the focused one.
	Key  string
	Kind RowKind
	Name string
	// Mark is the letter a person typed onto this row, empty when none is on it.
	Mark string
	// Matches are the stretches of the name a search picked out.
	Matches []Span
	Status  Status
	// Process says whether anything stands behind the row, which the bar draws as a dashed pipe
	// where a process has stopped and nothing at all where there is none.
	Process Process
	Repo    Repo
	Review  Review
	Forge   Forge
	// Selected says the row has been taken into a selection several rows wide.
	Selected bool
	// Unviewed says the row has said something nobody has read.
	Unviewed bool
	// Dim says the row is hidden or archived, which greys everything on it.
	Dim  bool
	Held bool
	// HeldWorking says a paused agent is working anyway, which the hold did not stop.
	HeldWorking bool
	UnreadMail  bool
	FailedRun   bool
	Scheduled   bool
	// HiddenCount is how many rows this one stands in for while they are out of sight.
	HiddenCount int
}
