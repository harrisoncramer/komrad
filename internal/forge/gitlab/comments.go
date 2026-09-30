package gitlab

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/lib/xslice"
)

// commentMarksPerQuery is how many merge requests one query reads the comment counts of.
const commentMarksPerQuery = 25

const commentMarkFields = `userNotesCount userDiscussionsCount`

const discussionsQuery = `query Discussions($path: ID!, $iid: String!) {
  project(fullPath: $path) {
    mergeRequest(iid: $iid) {
      diffRefs { baseSha }
      discussions(first: 100) {
        nodes {
          id resolved
          notes(first: 100) {
            nodes {
              id body system createdAt lastEditedAt
              author { username bot }
              position { filePath oldLine newLine diffRefs { headSha } }
            }
          }
        }
      }
    }
  }
}`

// CommentMarks reads each merge request's note and discussion counts, batched by alias, leaving
// out any GitLab does not have.
func (f *Forge) CommentMarks(ctx context.Context, repo git.RemoteURL, numbers []int) (map[int]string, error) {
	path, err := projectPath(repo.Path)
	if err != nil {
		return nil, err
	}
	if len(numbers) > forge.MaxCommentMarks {
		return nil, forge.ErrTooManyStandings.Saying("at most %d comment marks can be read in one call, not %d", forge.MaxCommentMarks, len(numbers))
	}

	marks := make(map[int]string, len(numbers))
	err = sendInBatches(ctx, numbers, commentMarksPerQuery, func(ctx context.Context, batch []int) (complexity, error) {
		var fields strings.Builder
		for _, number := range batch {
			fmt.Fprintf(&fields, "\n    m%d: mergeRequest(iid: \"%d\") { %s }", number, number, commentMarkFields)
		}
		operation := "query CommentMarks($path: ID!) {\n  project(fullPath: $path) {" + fields.String() + "\n  }\n  queryComplexity { score limit }\n}"

		var data struct {
			Project         map[string]*commentMarkNode `json:"project"`
			QueryComplexity complexity                  `json:"queryComplexity"`
		}
		answer, err := f.client.Query(ctx, operation, map[string]any{"path": path}, &data)
		if err != nil {
			return complexity{}, err
		}
		if err := failureOf(answer); err != nil {
			return complexity{}, err
		}
		if data.Project == nil {
			return complexity{}, fmt.Errorf("%w: gitlab has no project %s, which is also its answer for one the credential cannot see", forge.ErrNotFound, path)
		}

		for _, number := range batch {
			node := data.Project["m"+strconv.Itoa(number)]
			if node == nil {
				continue
			}

			marks[number] = fmt.Sprintf("%d/%d", node.UserNotesCount, node.UserDiscussionsCount)
		}

		return data.QueryComplexity, nil
	})
	if err != nil {
		return nil, err
	}

	return marks, nil
}

// ReviewThreads reads a merge request's discussions, leaving out the notes GitLab writes itself.
func (f *Forge) ReviewThreads(ctx context.Context, repo git.RemoteURL, number int) ([]forge.ReviewThread, error) {
	path, err := projectPath(repo.Path)
	if err != nil {
		return nil, err
	}

	var data struct {
		Project *struct {
			MergeRequest *discussionsNode `json:"mergeRequest"`
		} `json:"project"`
	}
	answer, err := f.client.Query(ctx, discussionsQuery, map[string]any{"path": path, "iid": strconv.Itoa(number)}, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Project == nil || data.Project.MergeRequest == nil {
		return nil, fmt.Errorf("%w: gitlab has no merge request %d in %s, which is also its answer for a project the credential cannot see", forge.ErrNotFound, number, path)
	}

	return data.Project.MergeRequest.threads(), nil
}

type commentMarkNode struct {
	UserNotesCount       int `json:"userNotesCount"`
	UserDiscussionsCount int `json:"userDiscussionsCount"`
}

type noteNode struct {
	ID           string     `json:"id"`
	Body         string     `json:"body"`
	System       bool       `json:"system"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastEditedAt *time.Time `json:"lastEditedAt"`
	Author       *struct {
		Username string `json:"username"`
		Bot      bool   `json:"bot"`
	} `json:"author"`
	Position *struct {
		FilePath string `json:"filePath"`
		OldLine  *int   `json:"oldLine"`
		NewLine  *int   `json:"newLine"`
		DiffRefs *struct {
			HeadSha string `json:"headSha"`
		} `json:"diffRefs"`
	} `json:"position"`
}

type discussionsNode struct {
	DiffRefs *struct {
		BaseSha string `json:"baseSha"`
	} `json:"diffRefs"`
	Discussions struct {
		Nodes []struct {
			ID       string `json:"id"`
			Resolved bool   `json:"resolved"`
			Notes    struct {
				Nodes []noteNode `json:"nodes"`
			} `json:"notes"`
		} `json:"nodes"`
	} `json:"discussions"`
}

func (n discussionsNode) threads() []forge.ReviewThread {
	base := ""
	if n.DiffRefs != nil {
		base = n.DiffRefs.BaseSha
	}

	threads := make([]forge.ReviewThread, 0, len(n.Discussions.Nodes))
	for _, discussion := range n.Discussions.Nodes {
		var notes []noteNode
		for _, note := range discussion.Notes.Nodes {
			if !note.System {
				notes = append(notes, note)
			}
		}
		if len(notes) == 0 {
			continue
		}

		thread := forge.ReviewThread{
			ProviderID: discussion.ID,
			BaseSHA:    base,
			Resolved:   discussion.Resolved,
		}
		if position := notes[0].Position; position != nil {
			thread.Path = position.FilePath
			if position.DiffRefs != nil {
				thread.CommitSHA = position.DiffRefs.HeadSha
			}
			switch {
			case position.NewLine != nil:
				thread.Side, thread.EndLine = forge.SideNew, *position.NewLine
			case position.OldLine != nil:
				thread.Side, thread.EndLine = forge.SideOld, *position.OldLine
			}
		}
		thread.Comments = xslice.Map(notes, noteNode.toComment)

		threads = append(threads, thread)
	}

	return threads
}

func (n noteNode) toComment() forge.ReviewComment {
	comment := forge.ReviewComment{
		ProviderID: n.ID,
		Body:       n.Body,
		WrittenAt:  n.CreatedAt,
		EditedAt:   n.LastEditedAt,
	}
	if n.Author != nil {
		comment.Login = n.Author.Username
		comment.Bot = n.Author.Bot
	}

	return comment
}
