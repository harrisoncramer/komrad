package panel

import (
	"strings"
	"time"

	"komrad/internal/errui"
	"komrad/internal/ui/paint/label"
)

// WorktreeEntry is one top-level entry of a directory.
type WorktreeEntry struct {
	Path  string
	Bytes int64
	Dir   bool
}

// WorktreeCommit is one of a branch's commits since its base.
type WorktreeCommit struct {
	SHA         string
	Subject     string
	CommittedAt time.Time
}

// WorktreeInspection is one directory read through git, or why it could not be.
type WorktreeInspection struct {
	// Pending says a fresh reading is on its way, and Answered that one has come at all.
	Pending  bool
	Answered bool
	// ReadAt is when the answer came.
	ReadAt time.Time
	// Item is the directory as the listing reports it, measured afresh by the read.
	Item      Worktree
	Entries   []WorktreeEntry
	Modified  []string
	Untracked []string
	BaseRef   string
	// BaseError is why git could not name a base, when BaseRef is empty for that reason.
	BaseError string
	Ahead     int
	Behind    int
	Pushed    bool
	Unpushed  int
	Unpulled  int
	// UnpulledLanded says the base already holds every unpulled commit.
	UnpulledLanded bool
	// Landed is how many of the branch's own commits the base already holds whole.
	Landed int
	// Sequencing says a merge, a rebase or a cherry-pick is part-way through.
	Sequencing  bool
	KeepsBranch bool
	Commits     []WorktreeCommit
	Err         error
}

// IsRead reports whether git has answered for the directory.
func (r WorktreeInspection) IsRead() bool {
	return r.Answered && (r.Err == nil || r.Item.Dir != "")
}

// Changes counts the files the directory has modified and left untracked, or says there are none.
func (r WorktreeInspection) Changes() string {
	var parts []string
	if n := len(r.Modified); n > 0 {
		parts = append(parts, label.Count(n, "modified", "modified"))
	}
	if n := len(r.Untracked); n > 0 {
		parts = append(parts, label.Count(n, "untracked", "untracked"))
	}
	if len(parts) == 0 {
		return "none"
	}

	return strings.Join(parts, ", ")
}

// UnpushedCommits is how many commits in the worktree no remote holds, and false where nothing
// was pushed and git cannot name the base to count from.
func (r WorktreeInspection) UnpushedCommits() (int, bool) {
	switch {
	case r.Pushed:
		return r.Unpushed, true
	case r.BaseRef == "":
		return 0, false
	}

	return r.Ahead, true
}

// IsSettled reports whether the answer is one that will never change.
func (r WorktreeInspection) IsSettled() bool {
	return r.Answered && errui.IsNotFound(r.Err)
}
