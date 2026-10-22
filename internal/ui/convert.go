package ui

import (
	"time"

	"komrad/internal/ui/panel"
	worktreev1 "komrad/proto/gen/worktree/v1"

	"komrad/lib/dates"
	"komrad/lib/xslice"
)

func worktreeInspectionFrom(read *worktreev1.GetWorktreeResponse, now time.Time) worktreeInspection {
	return worktreeInspection{
		answered: true,
		readAt:   now,
		item:     panel.WorktreeFrom(read.GetWorktree()),
		entries: xslice.Map(read.GetEntries(), func(entry *worktreev1.Entry) worktreeEntry {
			return worktreeEntry{
				path:  entry.GetPath(),
				bytes: entry.GetSizeBytes(),
				dir:   entry.GetDir(),
			}
		}),
		modified:       read.GetModified(),
		untracked:      read.GetUntracked(),
		baseRef:        read.GetBaseRef(),
		baseError:      read.GetBaseError(),
		ahead:          int(read.GetAhead()),
		behind:         int(read.GetBehind()),
		pushed:         read.GetPushed(),
		unpushed:       int(read.GetUnpushed()),
		unpulled:       int(read.GetUnpulled()),
		unpulledLanded: read.GetUnpulledLanded(),
		landed:         int(read.GetLandedCommits()),
		sequencing:     read.GetSequenceInProgress(),
		keepsBranch:    read.GetKeepsBranch(),
		commits: xslice.Map(read.GetCommits(), func(commit *worktreev1.Commit) worktreeCommit {
			return worktreeCommit{
				sha:         commit.GetSha(),
				subject:     commit.GetSubject(),
				committedAt: dates.Parse(commit.GetCommittedAt()),
			}
		}),
	}
}
