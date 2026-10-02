package gitlab

import (
	"context"
	"strconv"
	"strings"
	"time"

	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
	"komrad/internal/git"
	"komrad/lib/xslice"
)

// maxDiscussionPages is the most pages of discussions one read takes, past which the answer is
// marked truncated.
const maxDiscussionPages = 10

const reviewThreadsQuery = `query ReviewThreads($path: ID!, $iid: String!, $after: String) {
  project(fullPath: $path) {
    mergeRequest(iid: $iid) {
      discussions(first: 100, after: $after) {
        pageInfo { hasNextPage endCursor }
        nodes {
          id resolved
          truncatedDiffLines { text }
          notes(first: 100) {
            pageInfo { hasNextPage }
            nodes {
              id system internal body createdAt lastEditedAt lastEditedBy { username } url
              author { username bot }
              position { positionType filePath oldLine newLine diffRefs { headSha } }
            }
          }
        }
      }
    }
  }
}`

// ReviewThreads reads a merge request's discussions, those on the diff anchored to the line and
// commit GitLab has carried them to, and the rest as comments on the change as a whole.
func (f *Forge) ReviewThreads(ctx context.Context, repo git.RemoteURL, number int) (*forge.Threads, error) {
	path, err := projectPath(repo.Path)
	if err != nil {
		return nil, err
	}

	var discussions []discussionNode
	cursor := ""
	for range maxDiscussionPages {
		page, err := f.readDiscussions(ctx, path, number, cursor)
		if err != nil {
			return nil, err
		}
		discussions = append(discussions, page.Nodes...)
		if !page.PageInfo.HasNextPage {
			return threadsOf(discussions), nil
		}
		cursor = page.PageInfo.EndCursor
	}

	answer := threadsOf(discussions)
	answer.Truncated = true

	return answer, nil
}

// readDiscussions is one page of a merge request's discussions, starting after the cursor.
func (f *Forge) readDiscussions(ctx context.Context, path string, number int, cursor string) (*discussionsPage, error) {
	var data struct {
		Project *struct {
			MergeRequest *struct {
				Discussions discussionsPage `json:"discussions"`
			} `json:"mergeRequest"`
		} `json:"project"`
	}
	variables := map[string]any{"path": path, "iid": strconv.Itoa(number), "after": graphql.CursorOf(cursor)}
	answer, err := f.client.Query(ctx, reviewThreadsQuery, variables, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Project == nil || data.Project.MergeRequest == nil {
		return nil, noMergeRequest(number, path)
	}

	return &data.Project.MergeRequest.Discussions, nil
}

type authorNode struct {
	Username string `json:"username"`
	Bot      bool   `json:"bot"`
}

type positionNode struct {
	PositionType string `json:"positionType"`
	FilePath     string `json:"filePath"`
	OldLine      *int   `json:"oldLine"`
	NewLine      *int   `json:"newLine"`
	DiffRefs     struct {
		HeadSha string `json:"headSha"`
	} `json:"diffRefs"`
}

type noteNode struct {
	ID           string        `json:"id"`
	System       bool          `json:"system"`
	Internal     bool          `json:"internal"`
	Body         string        `json:"body"`
	CreatedAt    time.Time     `json:"createdAt"`
	LastEditedAt *time.Time    `json:"lastEditedAt"`
	LastEditedBy *userNode     `json:"lastEditedBy"`
	URL          string        `json:"url"`
	Author       *authorNode   `json:"author"`
	Position     *positionNode `json:"position"`
}

type discussionNode struct {
	ID                 string `json:"id"`
	Resolved           bool   `json:"resolved"`
	TruncatedDiffLines []struct {
		Text string `json:"text"`
	} `json:"truncatedDiffLines"`
	Notes struct {
		PageInfo struct {
			HasNextPage bool `json:"hasNextPage"`
		} `json:"pageInfo"`
		Nodes []noteNode `json:"nodes"`
	} `json:"notes"`
}

// discussionsPage is one page of a merge request's discussions as GitLab answers it.
type discussionsPage = graphql.Connection[discussionNode]

func threadsOf(discussions []discussionNode) *forge.Threads {
	answer := &forge.Threads{}

	for _, node := range discussions {
		answer.Truncated = answer.Truncated || node.Notes.PageInfo.HasNextPage

		written := xslice.Filter(node.Notes.Nodes, noteNode.isWritten)
		if len(written) == 0 {
			continue
		}

		thread := forge.ReviewThread{
			ProviderID: node.ID,
			URL:        written[0].URL,
			Resolved:   node.Resolved,
			Comments:   xslice.Map(written, func(note noteNode) forge.ReviewComment { return note.toComment(node.ID) }),
		}
		if position := written[0].Position; position != nil {
			thread.Path = position.FilePath
			thread.Line, thread.OldSide = position.line()
			if thread.Line > 0 {
				thread.CommitSHA = position.DiffRefs.HeadSha
				thread.Quote = node.quote()
			}
		}
		answer.Threads = append(answer.Threads, thread)
	}

	return answer
}

// isWritten says a person or a bot wrote the note, which leaves out GitLab's own notes of what
// changed and internal notes kept from anyone outside the project.
func (n noteNode) isWritten() bool {
	return !n.System && !n.Internal && strings.TrimSpace(n.Body) != ""
}

func (n noteNode) toComment(discussionID string) forge.ReviewComment {
	comment := forge.ReviewComment{
		ProviderID: n.ID,
		ReviewID:   discussionID,
		Body:       n.Body,
		WrittenAt:  n.CreatedAt,
	}
	// GitLab sets lastEditedAt on notes nobody edited, so only an editor marks a real edit.
	if n.LastEditedBy != nil {
		comment.EditedAt = n.LastEditedAt
	}
	if n.Author != nil {
		comment.Login = n.Author.Username
		comment.Bot = n.Author.Bot
	}

	return comment
}

// line is the line a diff note stands on and whether that line is on the old side, zero for a note
// on a whole file or an image.
func (p positionNode) line() (int, bool) {
	switch {
	case p.PositionType != "text":
		return 0, false
	case p.NewLine != nil:
		return *p.NewLine, false
	case p.OldLine != nil:
		return *p.OldLine, true
	}

	return 0, false
}

// quote is the last line of GitLab's excerpt of the diff, the line the note was written on,
// without its diff marker.
func (n discussionNode) quote() string {
	if len(n.TruncatedDiffLines) == 0 {
		return ""
	}

	last := n.TruncatedDiffLines[len(n.TruncatedDiffLines)-1].Text
	if last == "" {
		return ""
	}

	switch last[0] {
	case '+', '-', ' ':
		return last[1:]
	}

	return last
}
