package github

import (
	"context"
	_ "embed"

	"komrad/internal/forge"
	forgev1 "komrad/proto/gen/forge/v1"
)

//go:embed rereviewtarget.gql
var rereviewTargetQuery string

//go:embed requestreviews.gql
var requestReviewsMutation string

// RequestRereview asks every user whose latest verdict on the pull request asks for changes to
// review it again.
func (f *Forge) RequestRereview(ctx context.Context, address string) error {
	owner, name, number, err := parsePullURL(address)
	if err != nil {
		return err
	}

	var data struct {
		Repository *struct {
			PullRequest *struct {
				ID                       string `json:"id"`
				State                    string `json:"state"`
				LatestOpinionatedReviews struct {
					Nodes []opinionNode `json:"nodes"`
				} `json:"latestOpinionatedReviews"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	answer, err := f.client.Query(ctx, rereviewTargetQuery, map[string]any{
		"owner":  owner,
		"name":   name,
		"number": number,
	}, &data)
	if err != nil {
		return err
	}
	if err := failureOf(answer); err != nil {
		return err
	}
	if data.Repository == nil || data.Repository.PullRequest == nil {
		return noPullRequest(number, owner, name)
	}
	target := data.Repository.PullRequest
	if _, err := forge.ShouldChange(forge.ShouldChangeParams{
		Sentinel: forge.ErrNoRereview,
		Forge:    forgev1.Kind_KIND_GITHUB,
		Ref:      forge.Ref(forgev1.Kind_KIND_GITHUB, number),
		Merged:   target.State == "MERGED",
		Closed:   target.State == "CLOSED",
	}); err != nil {
		return err
	}

	var users []string
	for _, asker := range changeRequesters(target.LatestOpinionatedReviews.Nodes) {
		if asker.ID != "" {
			users = append(users, asker.ID)
		}
	}
	if len(users) == 0 {
		return forge.ErrNoRereview.Saying("nobody has asked for changes on #%d", number)
	}

	var requested struct{}
	answer, err = f.client.Query(ctx, requestReviewsMutation, map[string]any{"id": target.ID, "users": users}, &requested)
	if err != nil {
		return err
	}

	return mutationRefusal{sentinel: forge.ErrNoRereview, verb: "request reviews on"}.explain(answer, address, number)
}

// reviewDecisionOf is GitHub's reviewDecision in komrad's words, none for a null.
func reviewDecisionOf(decision *string) forge.ReviewDecision {
	if decision == nil {
		return forge.ReviewDecisionNone
	}

	switch *decision {
	case "APPROVED":
		return forge.ReviewDecisionApproved
	case "CHANGES_REQUESTED":
		return forge.ReviewDecisionChangesRequested
	case "REVIEW_REQUIRED":
		return forge.ReviewDecisionPending
	}

	return forge.ReviewDecisionNone
}

// opinionNode is one reviewer's latest approval or change request.
type opinionNode struct {
	State  string     `json:"state"`
	Author *actorNode `json:"author"`
}

// actorNode is a user GitHub names, or the empty node it answers for a team or a bot.
type actorNode struct {
	ID    string `json:"id"`
	Login string `json:"login"`
}

// decision is GitHub's review decision, read as a re-review requested once every user whose
// latest verdict asks for changes has received a new review request.
func (n mergeReadinessNode) decision() forge.ReviewDecision {
	decision := reviewDecisionOf(n.ReviewDecision)
	if decision != forge.ReviewDecisionChangesRequested {
		return decision
	}

	requested := make(map[string]bool)
	for _, request := range n.ReviewRequests.Nodes {
		if request.RequestedReviewer != nil && request.RequestedReviewer.Login != "" {
			requested[request.RequestedReviewer.Login] = true
		}
	}

	asked := false
	for _, asker := range changeRequesters(n.LatestOpinionatedReviews.Nodes) {
		if asker.Login == "" {
			continue
		}
		if !requested[asker.Login] {
			return decision
		}
		asked = true
	}
	if !asked {
		return decision
	}

	return forge.ReviewDecisionRereviewRequested
}

// changeRequesters is the users whose latest verdict asks for changes.
func changeRequesters(opinions []opinionNode) []actorNode {
	var askers []actorNode
	for _, opinion := range opinions {
		if opinion.State == "CHANGES_REQUESTED" && opinion.Author != nil {
			askers = append(askers, *opinion.Author)
		}
	}

	return askers
}

func (n *actorNode) login() string {
	if n == nil {
		return ""
	}

	return n.Login
}
