package ui

import (
	"time"

	"komrad/internal/ui/panel"
	worktreev1 "komrad/proto/gen/worktree/v1"

	"komrad/lib/dates"
	"komrad/lib/xslice"
)

func worktreeInspectionFrom(read *worktreev1.GetWorktreeResponse, now time.Time) panel.WorktreeInspection {
	return panel.WorktreeInspection{
		Answered: true,
		ReadAt:   now,
		Item:     panel.WorktreeFrom(read.GetWorktree()),
		Entries: xslice.Map(read.GetEntries(), func(entry *worktreev1.Entry) panel.WorktreeEntry {
			return panel.WorktreeEntry{
				Path:  entry.GetPath(),
				Bytes: entry.GetSizeBytes(),
				Dir:   entry.GetDir(),
			}
		}),
		Modified:       read.GetModified(),
		Untracked:      read.GetUntracked(),
		BaseRef:        read.GetBaseRef(),
		BaseError:      read.GetBaseError(),
		Ahead:          int(read.GetAhead()),
		Behind:         int(read.GetBehind()),
		Pushed:         read.GetPushed(),
		Unpushed:       int(read.GetUnpushed()),
		Unpulled:       int(read.GetUnpulled()),
		UnpulledLanded: read.GetUnpulledLanded(),
		Landed:         int(read.GetLandedCommits()),
		Sequencing:     read.GetSequenceInProgress(),
		KeepsBranch:    read.GetKeepsBranch(),
		Commits: xslice.Map(read.GetCommits(), func(commit *worktreev1.Commit) panel.WorktreeCommit {
			return panel.WorktreeCommit{
				SHA:         commit.GetSha(),
				Subject:     commit.GetSubject(),
				CommittedAt: dates.Parse(commit.GetCommittedAt()),
			}
		}),
	}
}
