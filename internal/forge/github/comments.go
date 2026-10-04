package github

import (
	"context"
	_ "embed"
	"strings"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
)

//go:embed reviewthreads.gql
var reviewThreadsQuery string

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
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Repository == nil {
		return nil, noRepository(variables)
	}
	if data.Repository.PullRequest == nil {
		return nil, noPullRequest(number, owner, name)
	}

	return data.Repository.PullRequest.threads(), nil
}

type authorNode struct {
	Typename string `json:"__typename"`
	Login    string `json:"login"`
}

type commentNode struct {
	ID                string      `json:"id"`
	URL               string      `json:"url"`
	Body              string      `json:"body"`
	CreatedAt         time.Time   `json:"createdAt"`
	LastEditedAt      *time.Time  `json:"lastEditedAt"`
	State             string      `json:"state"`
	AuthorAssociation string      `json:"authorAssociation"`
	Author            *authorNode `json:"author"`
	PullRequestReview *reviewNode `json:"pullRequestReview"`
	DiffHunk          string      `json:"diffHunk"`
	Commit            *commitNode `json:"commit"`
	OriginalCommit    *commitNode `json:"originalCommit"`
}

type reviewNode struct {
	ID string `json:"id"`
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
	DiffSide     string       `json:"diffSide"`
	SubjectType  string       `json:"subjectType"`
	Comments     commentsNode `json:"comments"`
}

type commitNode struct {
	OID string `json:"oid"`
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

		line, commit := node.anchorOf(submitted[0])
		answer.Threads = append(answer.Threads, forge.ReviewThread{
			ProviderID: node.ID,
			Path:       node.Path,
			Line:       line,
			OldSide:    line > 0 && node.DiffSide == "LEFT",
			CommitSHA:  commit,
			Quote:      quoteOfHunk(submitted[0].DiffHunk, line),
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
		Trusted:    isTrustedAssociation(n.AuthorAssociation),
		ReviewID:   n.ID,
		Body:       n.Body,
		WrittenAt:  n.CreatedAt,
		EditedAt:   n.LastEditedAt,
	}
	if n.PullRequestReview != nil {
		comment.ReviewID = n.PullRequestReview.ID
	}
	if n.Author != nil {
		comment.Login = n.Author.Login
		comment.Bot = n.Author.Typename == "Bot"
	}

	return comment
}

// isTrustedAssociation reports whether GitHub counts the author as one who may push to the repository.
func isTrustedAssociation(association string) bool {
	switch association {
	case "OWNER", "MEMBER", "COLLABORATOR":
		return true
	}

	return false
}

// anchorOf is the line a thread stands on and the commit that line is counted in: where GitHub
// has carried it to on the pull request's head, or where it was written once GitHub has lost it.
func (n reviewThreadNode) anchorOf(first commentNode) (int, string) {
	if n.SubjectType == "FILE" {
		return 0, ""
	}
	if n.Line != nil {
		return *n.Line, first.Commit.oid()
	}

	return xptr.Deref(n.OriginalLine), first.OriginalCommit.oid()
}

func (n *commitNode) oid() string {
	if n == nil {
		return ""
	}

	return n.OID
}

// quoteOfHunk is the line a comment was written on, which GitHub's hunk ends with, with its diff
// marker taken off.
func quoteOfHunk(hunk string, line int) string {
	if line <= 0 {
		return ""
	}

	rows := strings.Split(strings.TrimRight(hunk, "\n"), "\n")
	last := rows[len(rows)-1]
	if strings.HasPrefix(last, "@@") || last == "" {
		return ""
	}

	switch last[0] {
	case '+', '-', ' ':
		return last[1:]
	}

	return last
}
