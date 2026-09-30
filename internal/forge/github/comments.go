package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
)

const reviewThreadsQuery = `query ReviewThreads($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      reviewThreads(first: 100) {
        pageInfo { hasNextPage }
        nodes {
          id isResolved path line originalLine
          comments(first: 100) {
            pageInfo { hasNextPage }
            nodes { id url body createdAt lastEditedAt state author { __typename login } }
          }
        }
      }
      comments(last: 100) {
        pageInfo { hasPreviousPage }
        nodes { id url body createdAt lastEditedAt author { __typename login } }
      }
      reviews(last: 100, states: [COMMENTED, APPROVED, CHANGES_REQUESTED, DISMISSED]) {
        pageInfo { hasPreviousPage }
        nodes { id url body createdAt lastEditedAt author { __typename login } }
      }
    }
  }
}`

// ReviewThreads reads a pull request's submitted review threads, then its conversation comments
// and review bodies as comments on the change as a whole.
func (f *Forge) ReviewThreads(ctx context.Context, repo git.RemoteURL, number int) (*forge.Threads, error) {
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

type authorNode struct {
	Typename string `json:"__typename"`
	Login    string `json:"login"`
}

type commentNode struct {
	ID           string      `json:"id"`
	URL          string      `json:"url"`
	Body         string      `json:"body"`
	CreatedAt    time.Time   `json:"createdAt"`
	LastEditedAt *time.Time  `json:"lastEditedAt"`
	State        string      `json:"state"`
	Author       *authorNode `json:"author"`
}

type pageInfo struct {
	HasNextPage     bool `json:"hasNextPage"`
	HasPreviousPage bool `json:"hasPreviousPage"`
}

func (p pageInfo) more() bool {
	return p.HasNextPage || p.HasPreviousPage
}

type commentsNode struct {
	PageInfo pageInfo      `json:"pageInfo"`
	Nodes    []commentNode `json:"nodes"`
}

type reviewThreadNode struct {
	ID           string       `json:"id"`
	IsResolved   bool         `json:"isResolved"`
	Path         string       `json:"path"`
	Line         *int         `json:"line"`
	OriginalLine *int         `json:"originalLine"`
	Comments     commentsNode `json:"comments"`
}

type reviewThreadsNode struct {
	ReviewThreads struct {
		PageInfo pageInfo           `json:"pageInfo"`
		Nodes    []reviewThreadNode `json:"nodes"`
	} `json:"reviewThreads"`
	Comments commentsNode `json:"comments"`
	Reviews  commentsNode `json:"reviews"`
}

func (n reviewThreadsNode) threads() *forge.Threads {
	answer := &forge.Threads{
		Truncated: n.ReviewThreads.PageInfo.more() || n.Comments.PageInfo.more() || n.Reviews.PageInfo.more(),
	}

	for _, node := range n.ReviewThreads.Nodes {
		answer.Truncated = answer.Truncated || node.Comments.PageInfo.more()

		var submitted []commentNode
		for _, comment := range node.Comments.Nodes {
			if comment.State != "PENDING" {
				submitted = append(submitted, comment)
			}
		}
		if len(submitted) == 0 {
			continue
		}

		line := xptr.Deref(node.Line)
		if line == 0 {
			line = xptr.Deref(node.OriginalLine)
		}
		answer.Threads = append(answer.Threads, forge.ReviewThread{
			ProviderID: node.ID,
			Path:       node.Path,
			Line:       line,
			URL:        submitted[0].URL,
			Resolved:   node.IsResolved,
			Comments:   xslice.Map(submitted, commentNode.toComment),
		})
	}

	for _, node := range append(n.Reviews.Nodes, n.Comments.Nodes...) {
		if strings.TrimSpace(node.Body) == "" {
			continue
		}

		answer.Threads = append(answer.Threads, forge.ReviewThread{
			ProviderID: node.ID,
			URL:        node.URL,
			Comments:   []forge.ReviewComment{node.toComment()},
		})
	}

	return answer
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
