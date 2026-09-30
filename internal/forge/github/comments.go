package github

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
)

const commentMarkFields = `reviewThreads { totalCount } comments(last: 1) { totalCount nodes { updatedAt } } reviews(last: 1) { totalCount nodes { updatedAt } }`

const reviewThreadsQuery = `query ReviewThreads($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      baseRefOid
      reviewThreads(first: 100) {
        nodes {
          id isResolved isOutdated path diffSide originalLine originalStartLine
          comments(first: 100) {
            nodes { id body createdAt lastEditedAt diffHunk originalCommit { oid } author { __typename login } }
          }
        }
      }
      comments(last: 100) {
        nodes { id body createdAt lastEditedAt author { __typename login } }
      }
      reviews(last: 100) {
        nodes { id body createdAt lastEditedAt author { __typename login } }
      }
    }
  }
}`

// CommentMarks reads each pull request's thread count and the stamps of its latest comment and
// review in one query, by alias, leaving out any GitHub does not have.
func (f *Forge) CommentMarks(ctx context.Context, repo git.RemoteURL, numbers []int) (map[int]string, error) {
	owner, name, err := ownerAndName(repo.Path)
	if err != nil {
		return nil, err
	}
	if len(numbers) > forge.MaxPullRequestsPerCall {
		return nil, forge.ErrTooManyPullRequests.Saying("at most %d comment marks can be read in one call, not %d", forge.MaxPullRequestsPerCall, len(numbers))
	}

	var fields strings.Builder
	for _, number := range numbers {
		fmt.Fprintf(&fields, "\n    p%d: pullRequest(number: %d) { %s }", number, number, commentMarkFields)
	}
	operation := "query CommentMarks($owner: String!, $name: String!) {\n  repository(owner: $owner, name: $name) {" + fields.String() + "\n  }\n}"

	var data struct {
		Repository map[string]*commentMarkNode `json:"repository"`
	}
	variables := map[string]any{"owner": owner, "name": name}
	answer, err := f.client.Query(ctx, operation, variables, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer, "repository"); err != nil {
		return nil, err
	}
	if data.Repository == nil {
		return nil, noRepository(variables)
	}

	marks := make(map[int]string, len(numbers))
	for _, number := range numbers {
		node := data.Repository["p"+strconv.Itoa(number)]
		if node == nil {
			continue
		}

		marks[number] = node.mark()
	}

	return marks, nil
}

// ReviewThreads reads a pull request's review threads, then its conversation comments and the
// bodies of its reviews as comments on the change as a whole.
func (f *Forge) ReviewThreads(ctx context.Context, repo git.RemoteURL, number int) ([]forge.ReviewThread, error) {
	owner, name, err := ownerAndName(repo.Path)
	if err != nil {
		return nil, err
	}

	var data struct {
		Repository *struct {
			PullRequest *reviewThreadsNode `json:"pullRequest"`
		} `json:"repository"`
	}
	variables := map[string]any{"owner": owner, "name": name, "number": number}
	answer, err := f.client.Query(ctx, reviewThreadsQuery, variables, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer, ""); err != nil {
		return nil, err
	}
	if data.Repository == nil {
		return nil, noRepository(variables)
	}
	if data.Repository.PullRequest == nil {
		return nil, fmt.Errorf("%w: github has no pull request %d in %s/%s", forge.ErrNotFound, number, owner, name)
	}

	return data.Repository.PullRequest.threads(), nil
}

type commentMarkNode struct {
	ReviewThreads struct {
		TotalCount int `json:"totalCount"`
	} `json:"reviewThreads"`
	Comments stampedCount `json:"comments"`
	Reviews  stampedCount `json:"reviews"`
}

type stampedCount struct {
	TotalCount int `json:"totalCount"`
	Nodes      []struct {
		UpdatedAt time.Time `json:"updatedAt"`
	} `json:"nodes"`
}

func (c stampedCount) latest() string {
	if len(c.Nodes) == 0 {
		return ""
	}

	return c.Nodes[len(c.Nodes)-1].UpdatedAt.UTC().Format(time.RFC3339)
}

func (n commentMarkNode) mark() string {
	return fmt.Sprintf("%d/%d/%s/%d/%s",
		n.ReviewThreads.TotalCount,
		n.Comments.TotalCount, n.Comments.latest(),
		n.Reviews.TotalCount, n.Reviews.latest())
}

type authorNode struct {
	Typename string `json:"__typename"`
	Login    string `json:"login"`
}

type commentNode struct {
	ID             string      `json:"id"`
	Body           string      `json:"body"`
	CreatedAt      time.Time   `json:"createdAt"`
	LastEditedAt   *time.Time  `json:"lastEditedAt"`
	DiffHunk       string      `json:"diffHunk"`
	OriginalCommit *oidNode    `json:"originalCommit"`
	Author         *authorNode `json:"author"`
}

type oidNode struct {
	Oid string `json:"oid"`
}

type reviewThreadNode struct {
	ID                string `json:"id"`
	IsResolved        bool   `json:"isResolved"`
	IsOutdated        bool   `json:"isOutdated"`
	Path              string `json:"path"`
	DiffSide          string `json:"diffSide"`
	OriginalLine      *int   `json:"originalLine"`
	OriginalStartLine *int   `json:"originalStartLine"`
	Comments          struct {
		Nodes []commentNode `json:"nodes"`
	} `json:"comments"`
}

type reviewThreadsNode struct {
	BaseRefOid    string `json:"baseRefOid"`
	ReviewThreads struct {
		Nodes []reviewThreadNode `json:"nodes"`
	} `json:"reviewThreads"`
	Comments struct {
		Nodes []commentNode `json:"nodes"`
	} `json:"comments"`
	Reviews struct {
		Nodes []commentNode `json:"nodes"`
	} `json:"reviews"`
}

func (n reviewThreadsNode) threads() []forge.ReviewThread {
	threads := make([]forge.ReviewThread, 0, len(n.ReviewThreads.Nodes)+len(n.Comments.Nodes))
	for _, node := range n.ReviewThreads.Nodes {
		if len(node.Comments.Nodes) == 0 {
			continue
		}

		first := node.Comments.Nodes[0]
		thread := forge.ReviewThread{
			ProviderID: node.ID,
			Path:       node.Path,
			Side:       sideOf(node.DiffSide),
			BaseSHA:    n.BaseRefOid,
			EndLine:    xptr.Deref(node.OriginalLine),
			StartLine:  xptr.Deref(node.OriginalStartLine),
			DiffHunk:   first.DiffHunk,
			Resolved:   node.IsResolved,
			Outdated:   node.IsOutdated,
		}
		if first.OriginalCommit != nil {
			thread.CommitSHA = first.OriginalCommit.Oid
		}
		thread.Comments = xslice.Map(node.Comments.Nodes, commentNode.toComment)

		threads = append(threads, thread)
	}

	for _, node := range append(n.Reviews.Nodes, n.Comments.Nodes...) {
		if strings.TrimSpace(node.Body) == "" {
			continue
		}

		threads = append(threads, forge.ReviewThread{
			ProviderID: node.ID,
			BaseSHA:    n.BaseRefOid,
			Comments:   []forge.ReviewComment{node.toComment()},
		})
	}

	return threads
}

func (n commentNode) toComment() forge.ReviewComment {
	comment := forge.ReviewComment{
		ProviderID: n.ID,
		Body:       n.Body,
		WrittenAt:  n.CreatedAt,
		EditedAt:   n.LastEditedAt,
	}
	if n.Author != nil {
		comment.Login = n.Author.Login
		comment.Bot = n.Author.Typename == "Bot"
	}

	return comment
}

func sideOf(diffSide string) forge.Side {
	if diffSide == "LEFT" {
		return forge.SideOld
	}

	return forge.SideNew
}
