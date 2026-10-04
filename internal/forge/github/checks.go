package github

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"time"

	"komrad/internal/forge"
	"komrad/lib/xptr"
)

// checkListFields is the first hundred checks on a pull request's head commit, each asked whether
// the pull request named by $pullRequest requires it.
//
//go:embed checklist.gql
var checkListFields string

// checkListNode is the checks GitHub lists on a pull request's newest commit.
type checkListNode struct {
	Commits struct {
		Nodes []struct {
			Commit struct {
				Oid               string `json:"oid"`
				StatusCheckRollup *struct {
					Contexts struct {
						PageInfo struct {
							HasNextPage bool `json:"hasNextPage"`
						} `json:"pageInfo"`
						Nodes []checkNode `json:"nodes"`
					} `json:"contexts"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

// checkNode is one check run or commit status GitHub lists on a commit.
type checkNode struct {
	Typename    string     `json:"__typename"`
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Conclusion  *string    `json:"conclusion"`
	StartedAt   *time.Time `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt"`
	DetailsURL  *string    `json:"detailsUrl"`
	CheckSuite  *struct {
		App *struct {
			Name string `json:"name"`
		} `json:"app"`
		WorkflowRun *struct {
			Workflow struct {
				Name string `json:"name"`
			} `json:"workflow"`
		} `json:"workflowRun"`
	} `json:"checkSuite"`
	Context    string     `json:"context"`
	State      string     `json:"state"`
	TargetURL  *string    `json:"targetUrl"`
	CreatedAt  *time.Time `json:"createdAt"`
	IsRequired bool       `json:"isRequired"`
}

// readCheckListings lists the checks on the head of each open pull request in the reading, in one
// query by alias.
func (f *Forge) readCheckListings(ctx context.Context, reading *forge.Reading) error {
	variables := map[string]any{}
	var declared, fields strings.Builder
	var open []*forge.PullRequest
	for i := range reading.PullRequests {
		pullRequest := &reading.PullRequests[i]
		if pullRequest.State != "OPEN" {
			continue
		}
		alias := "p" + strconv.Itoa(len(open))
		variables[alias] = pullRequest.ProviderID
		fmt.Fprintf(&declared, ", $%s: ID!", alias)
		fmt.Fprintf(&fields, "\n  %s: node(id: $%s) { ... on PullRequest { %s } }", alias, alias, strings.ReplaceAll(checkListFields, "$pullRequest", "$"+alias))
		open = append(open, pullRequest)
	}
	if len(open) == 0 {
		return nil
	}
	operation := "query ReadChecks(" + strings.TrimPrefix(declared.String(), ", ") + ") {" + fields.String() + "\n}"

	var data map[string]*checkListNode
	answer, err := f.client.Query(ctx, operation, variables, &data)
	if err != nil {
		return err
	}
	if err := failureOf(answer); err != nil {
		return err
	}

	if answer.RateLimit.Known {
		reading.RateLimit = answer.RateLimit
	}
	for i, pullRequest := range open {
		pullRequest.MergeReadiness.Listing = data["p"+strconv.Itoa(i)].listingOf(pullRequest.HeadSHA)
	}

	return nil
}

// listingOf is the checks on the head commit, nil when GitHub answered for another commit or no
// pull request.
func (n *checkListNode) listingOf(headSHA string) *forge.CheckListing {
	if n == nil || len(n.Commits.Nodes) == 0 {
		return nil
	}
	commit := n.Commits.Nodes[len(n.Commits.Nodes)-1].Commit
	if commit.Oid != headSHA {
		return nil
	}

	listing := &forge.CheckListing{}
	if commit.StatusCheckRollup == nil {
		return listing
	}
	listing.Truncated = commit.StatusCheckRollup.Contexts.PageInfo.HasNextPage
	for _, node := range commit.StatusCheckRollup.Contexts.Nodes {
		if check, ok := node.check(); ok {
			listing.Checks = append(listing.Checks, check)
		}
	}

	return listing
}

// check is the node in komrad's words, false for a kind of check GitHub added since.
func (n checkNode) check() (forge.Check, bool) {
	switch n.Typename {
	case "CheckRun":
		state, conclusion := checkRunStateOf(n.Status, n.Conclusion)
		return forge.Check{
			ProviderID: n.ID,
			Kind:       forge.CheckKindRun,
			Source:     n.source(),
			Name:       n.Name,
			State:      state,
			Conclusion: conclusion,
			StartedAt:  n.StartedAt,
			FinishedAt: n.CompletedAt,
			Required:   n.IsRequired,
			URL:        xptr.Deref(n.DetailsURL),
		}, true
	case "StatusContext":
		check := forge.Check{
			ProviderID: n.ID,
			Kind:       forge.CheckKindStatus,
			Name:       n.Context,
			Required:   n.IsRequired,
			URL:        xptr.Deref(n.TargetURL),
		}
		switch n.State {
		case "EXPECTED":
			check.State = forge.CheckStateWaiting
		case "PENDING":
			check.State = forge.CheckStateRunning
			check.StartedAt = n.CreatedAt
		default:
			check.State = forge.CheckStateCompleted
			check.Conclusion = strings.ToLower(n.State)
			check.FinishedAt = n.CreatedAt
		}

		return check, true
	}

	return forge.Check{}, false
}

// source is the workflow that ran the check run, or the app when no workflow did.
func (n checkNode) source() string {
	if n.CheckSuite == nil {
		return ""
	}
	if n.CheckSuite.WorkflowRun != nil {
		return n.CheckSuite.WorkflowRun.Workflow.Name
	}
	if n.CheckSuite.App != nil {
		return n.CheckSuite.App.Name
	}

	return ""
}

// checkRunStateOf is a check run's status in komrad's words, with its conclusion lowercased once
// it has completed.
func checkRunStateOf(status string, conclusion *string) (forge.CheckState, string) {
	switch status {
	case "IN_PROGRESS":
		return forge.CheckStateRunning, ""
	case "COMPLETED":
		return forge.CheckStateCompleted, strings.ToLower(xptr.Deref(conclusion))
	case "WAITING", "REQUESTED":
		return forge.CheckStateWaiting, ""
	}

	return forge.CheckStateQueued, ""
}
