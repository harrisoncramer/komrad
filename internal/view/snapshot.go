package view

// RowKind is what one line of the agents list stands for.
type RowKind int

const (
	KindWorkflow RowKind = iota
	KindAgent
	KindChild
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
)

// Review is where the local review of an agent's work stands.
type Review int

const (
	ReviewNone Review = iota
	ReviewOpen
	ReviewApproved
	ReviewChangesRequested
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

// Snapshot is everything the agents list draws itself from.
type Snapshot struct {
	Rows []Row
	// Cursor is the row the keyboard is standing on, which no row of its own reports.
	Cursor int
	// NameWidth is the column names are cut to, falling back to a default when it is unset.
	NameWidth int
}

// Row is one line of the agents list as the panel found it.
type Row struct {
	Kind RowKind
	Name string
	// Mark is the letter a person typed onto this row, empty when none is on it.
	Mark   string
	Status Status
	Repo   Repo
	Review Review
	Forge  Forge
	// Selected says the row has been taken into a selection several rows wide.
	Selected bool
	// Unviewed says the row has said something nobody has read.
	Unviewed bool
	// Dim says the row is hidden or archived, which greys everything on it.
	Dim        bool
	Held       bool
	UnreadMail bool
	FailedRun  bool
	Scheduled  bool
	// HiddenCount is how many rows this one stands in for while they are out of sight.
	HiddenCount int
}
