package forge

import (
	"strings"
	"time"
)

// Side is which side of a diff a review thread's lines were counted on.
type Side string

const (
	// SideNone is a thread on the change as a whole.
	SideNone Side = ""
	// SideOld is a thread on lines of the base.
	SideOld Side = "old"
	// SideNew is a thread on lines of the head.
	SideNew Side = "new"
)

// ReviewThread is one conversation a forge holds on a pull request, as the forge holds it.
type ReviewThread struct {
	ProviderID string
	Path       string
	Side       Side
	CommitSHA  string
	BaseSHA    string
	StartLine  int
	EndLine    int
	DiffHunk   string
	Resolved   bool
	Outdated   bool
	Comments   []ReviewComment
}

// ReviewComment is one comment in a forge review thread.
type ReviewComment struct {
	ProviderID string
	Login      string
	Bot        bool
	Body       string
	WrittenAt  time.Time
	EditedAt   *time.Time
}

// QuotedLines is the lines the thread was written against, read off the end of its diff hunk on its side without their diff markers.
func (t ReviewThread) QuotedLines() []string {
	if t.DiffHunk == "" || t.Side == SideNone || t.EndLine <= 0 {
		return nil
	}

	want := 1
	if t.StartLine > 0 && t.StartLine <= t.EndLine {
		want = t.EndLine - t.StartLine + 1
	}

	skip := byte('-')
	if t.Side == SideOld {
		skip = '+'
	}

	var side []string
	for line := range strings.SplitSeq(strings.TrimRight(t.DiffHunk, "\n"), "\n") {
		if line == "" {
			side = append(side, "")

			continue
		}

		switch line[0] {
		case '@', '\\', skip:
			continue
		case ' ', '+', '-':
			side = append(side, line[1:])
		default:
			side = append(side, line)
		}
	}

	if len(side) < want {
		return side
	}

	return side[len(side)-want:]
}
