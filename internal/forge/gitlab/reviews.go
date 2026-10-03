package gitlab

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"komrad/internal/forge"
)

// reviewStateRequestedChanges is the reviewer state GitLab records when one asks for changes.
const reviewStateRequestedChanges = "REQUESTED_CHANGES"

const rereviewTargetQuery = `query RereviewTarget($path: ID!, $iid: String!) {
  project(fullPath: $path) { mergeRequest(iid: $iid) { state reviewers { nodes { id mergeRequestInteraction { reviewState } } } } }
}`

const rereviewMutation = `mutation Rereview($path: ID!, $iid: String!, $user: UserID!) {
  mergeRequestReviewerRereview(input: {projectPath: $path, iid: $iid, userId: $user}) { errors }
}`

// RequestRereview asks every reviewer GitLab records as requesting changes on the merge request
// to review it again.
func (f *Forge) RequestRereview(ctx context.Context, address string) error {
	path, number, err := parseMergeRequestURL(address)
	if err != nil {
		return err
	}

	var data struct {
		Project *struct {
			MergeRequest *struct {
				State     string `json:"state"`
				Reviewers struct {
					Nodes []struct {
						ID                      string `json:"id"`
						MergeRequestInteraction *struct {
							ReviewState string `json:"reviewState"`
						} `json:"mergeRequestInteraction"`
					} `json:"nodes"`
				} `json:"reviewers"`
			} `json:"mergeRequest"`
		} `json:"project"`
	}
	iid := strconv.Itoa(number)
	answer, err := f.client.Query(ctx, rereviewTargetQuery, map[string]any{"path": path, "iid": iid}, &data)
	if err != nil {
		return err
	}
	if err := failureOf(answer); err != nil {
		return err
	}
	if data.Project == nil || data.Project.MergeRequest == nil {
		return noMergeRequest(number, path)
	}

	target := data.Project.MergeRequest
	if _, err := forge.ShouldChange(forge.ShouldChangeParams{
		Sentinel: forge.ErrNoRereview,
		Forge:    forge.KindGitLab,
		Ref:      forge.Ref(forge.KindGitLab, number),
		Merged:   target.State == "merged",
		Closed:   target.State == "closed",
	}); err != nil {
		return err
	}

	var users []string
	for _, reviewer := range target.Reviewers.Nodes {
		if reviewer.MergeRequestInteraction != nil && reviewer.MergeRequestInteraction.ReviewState == reviewStateRequestedChanges {
			users = append(users, reviewer.ID)
		}
	}
	if len(users) == 0 {
		return forge.ErrNoRereview.Saying("nobody has asked for changes on !%d", number)
	}

	for _, user := range users {
		if err := f.rereview(ctx, path, iid, user); err != nil {
			return err
		}
	}

	return nil
}

// rereview asks one reviewer to review the merge request again.
func (f *Forge) rereview(ctx context.Context, path, iid, user string) error {
	var data struct {
		MergeRequestReviewerRereview *struct {
			Errors []string `json:"errors"`
		} `json:"mergeRequestReviewerRereview"`
	}
	answer, err := f.client.Query(ctx, rereviewMutation, map[string]any{"path": path, "iid": iid, "user": user}, &data)
	if err != nil {
		return err
	}
	if data.MergeRequestReviewerRereview == nil {
		if err := failureOf(answer); err != nil {
			return err
		}

		return fmt.Errorf("%w: gitlab has no merge request %s in %s", forge.ErrNotFound, iid, path)
	}
	if said := data.MergeRequestReviewerRereview.Errors; len(said) > 0 {
		return forge.ErrNoRereview.Saying("gitlab will not ask for another review of !%s: %s", iid, strings.Join(said, "; "))
	}

	return failureOf(answer)
}

// decision folds the approvals into komrad's word, a change request first, then approvals
// still owed, then an approval given.
func (n mergeReadinessNode) decision() forge.ReviewDecision {
	for _, reviewer := range n.Reviewers.Nodes {
		if reviewer.MergeRequestInteraction != nil && reviewer.MergeRequestInteraction.ReviewState == reviewStateRequestedChanges {
			return forge.ReviewDecisionChangesRequested
		}
	}

	switch {
	case n.ApprovalsLeft > 0:
		return forge.ReviewDecisionPending
	case n.ApprovedBy.Count > 0:
		return forge.ReviewDecisionApproved
	}

	return forge.ReviewDecisionNone
}
