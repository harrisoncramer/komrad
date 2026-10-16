package github

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"komrad/internal/forge"
	"komrad/lib/xslice"
)

//go:embed mergeplan.gql
var mergePlanQuery string

// messageSettings is what a repository writes into the commits its merges make.
type messageSettings struct {
	MergeCommitTitle         string `json:"mergeCommitTitle"`
	MergeCommitMessage       string `json:"mergeCommitMessage"`
	SquashMergeCommitTitle   string `json:"squashMergeCommitTitle"`
	SquashMergeCommitMessage string `json:"squashMergeCommitMessage"`
}

// planTarget is a pull request about to merge, with what its merge commits are written from.
type planTarget struct {
	Number              int      `json:"number"`
	State               string   `json:"state"`
	Title               string   `json:"title"`
	Body                string   `json:"body"`
	HeadRefName         string   `json:"headRefName"`
	HeadRefOid          string   `json:"headRefOid"`
	BaseRef             *baseRef `json:"baseRef"`
	HeadRepositoryOwner *struct {
		Login string `json:"login"`
	} `json:"headRepositoryOwner"`
	Commits struct {
		TotalCount int                 `json:"totalCount"`
		Nodes      []pullRequestCommit `json:"nodes"`
	} `json:"commits"`
}

// pullRequestCommit is one of a pull request's commits.
type pullRequestCommit struct {
	Commit struct {
		MessageHeadline string `json:"messageHeadline"`
		MessageBody     string `json:"messageBody"`
	} `json:"commit"`
}

// PlanMerge reads the methods the pull request's repository and base branch allow, with the
// message GitHub would write for each.
func (f *Forge) PlanMerge(ctx context.Context, address string) (*forge.MergePlan, error) {
	owner, name, number, err := parsePullURL(address)
	if err != nil {
		return nil, err
	}

	var data struct {
		Repository *struct {
			mergeSettings
			messageSettings
			PullRequest *planTarget `json:"pullRequest"`
		} `json:"repository"`
	}
	answer, err := f.client.Query(ctx, mergePlanQuery, map[string]any{
		"owner":  owner,
		"name":   name,
		"number": number,
	}, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Repository == nil || data.Repository.PullRequest == nil {
		return nil, noPullRequest(number, owner, name)
	}

	target := data.Repository.PullRequest
	if err := finishedRefusal(target.State, number); err != nil {
		return nil, err
	}

	allowed := data.Repository.allowedMethods(target.BaseRef)

	return &forge.MergePlan{
		Options: xslice.Map(mergeMethodOrder, func(method string) forge.MergeOption {
			return forge.MergeOption{
				Method:  mergeMethodOf[method],
				Allowed: allowed[method],
				Message: data.Repository.messageFor(method, target),
			}
		}),
		DefaultMethod: mergeMethodOf[data.Repository.chooseMethod(target.BaseRef)],
		HeadSHA:       target.HeadRefOid,
	}, nil
}

// messageFor is the commit message GitHub writes when it merges the pull request by the method,
// nil for a rebase, which writes no commit of its own.
func (s messageSettings) messageFor(method string, target *planTarget) *forge.CommitMessage {
	numbered := fmt.Sprintf("%s (#%d)", target.Title, target.Number)

	switch method {
	case "MERGE":
		message := &forge.CommitMessage{Headline: numbered}
		if s.MergeCommitTitle == "MERGE_MESSAGE" {
			message.Headline = fmt.Sprintf("Merge pull request #%d from %s", target.Number, target.headLabel())
		}
		switch s.MergeCommitMessage {
		case "PR_TITLE":
			message.Body = target.Title
		case "PR_BODY":
			message.Body = target.Body
		}

		return message
	case "SQUASH":
		single := s.SquashMergeCommitTitle == "COMMIT_OR_PR_TITLE" && target.Commits.TotalCount == 1 && len(target.Commits.Nodes) == 1
		message := &forge.CommitMessage{Headline: numbered}
		if single {
			message.Headline = fmt.Sprintf("%s (#%d)", target.Commits.Nodes[0].Commit.MessageHeadline, target.Number)
		}
		switch s.SquashMergeCommitMessage {
		case "PR_BODY":
			message.Body = target.Body
		case "COMMIT_MESSAGES":
			message.Body = target.commitMessages(single)
		}

		return message
	}

	return nil
}

// headLabel is the head branch as GitHub names it in a merge commit, prefixed by its owner.
func (t *planTarget) headLabel() string {
	if t.HeadRepositoryOwner == nil {
		return t.HeadRefName
	}

	return t.HeadRepositoryOwner.Login + "/" + t.HeadRefName
}

// commitMessages is the squash body GitHub writes from the pull request's commits, the lone
// commit's body when its headline already titles the squash.
func (t *planTarget) commitMessages(single bool) string {
	if single {
		return t.Commits.Nodes[0].Commit.MessageBody
	}

	entries := make([]string, 0, len(t.Commits.Nodes))
	for _, node := range t.Commits.Nodes {
		entry := "* " + node.Commit.MessageHeadline
		if node.Commit.MessageBody != "" {
			entry += "\n\n" + node.Commit.MessageBody
		}
		entries = append(entries, entry)
	}

	return strings.Join(entries, "\n\n")
}
