package assemble

import (
	"cmp"
	"slices"
	"strings"

	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// Offers is the forge's review threads on an agent's current open pull request, oldest first.
func Offers(w World, agentID string) []*pullrequestv1.ForgeThread {
	agent, ok := w.Agents[agentID]
	if !ok {
		return nil
	}

	current := getOpenPullRequest(groupPullRequestsByBranch(w.PullRequests)[getBranchForAgent(w, agent)])
	if current == nil {
		return nil
	}

	return groupOffersByPullRequest(w.ForgeThreads)[current.GetId()]
}

func isBotOffer(thread *pullrequestv1.ForgeThread) bool {
	comments := thread.GetComments()

	return len(comments) > 0 && !slices.ContainsFunc(comments, func(comment *pullrequestv1.ForgeComment) bool { return !comment.GetBot() })
}

func isCountedOffer(thread *pullrequestv1.ForgeThread) bool {
	comments := thread.GetComments()

	return thread.GetPath() != "" && !thread.GetResolved() && len(comments) > 0 && !comments[0].GetYours() && !isBotOffer(thread)
}

func getOpenPullRequest(pullRequests []*pullrequestv1.PullRequest) *pullrequestv1.PullRequest {
	if len(pullRequests) == 0 || finishedRank(pullRequests[0]) != 0 {
		return nil
	}

	return pullRequests[0]
}

func groupOffersByPullRequest(threads map[string]*pullrequestv1.ForgeThread) map[string][]*pullrequestv1.ForgeThread {
	byPullRequest := map[string][]*pullrequestv1.ForgeThread{}
	for _, thread := range threads {
		byPullRequest[thread.GetPullRequestId()] = append(byPullRequest[thread.GetPullRequestId()], thread)
	}

	for _, listed := range byPullRequest {
		slices.SortFunc(listed, func(a, b *pullrequestv1.ForgeThread) int {
			return cmp.Or(
				strings.Compare(a.GetWrittenAt(), b.GetWrittenAt()),
				strings.Compare(a.GetId(), b.GetId()),
			)
		})
	}

	return byPullRequest
}

func setOfferCounts(row *agentv1.AgentRow, offers []*pullrequestv1.ForgeThread) {
	external := 0
	for _, offer := range offers {
		if isCountedOffer(offer) {
			external++
		}
	}
	if external == 0 {
		return
	}

	if row.PersonComments == nil {
		row.PersonComments = &agentv1.CommentCounts{}
	}
	row.PersonComments.External = int32(external)
}
