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

// OfferState is where a forge thread stands against the review thread the person accepted it into.
type OfferState int

const (
	// OfferOpen is a forge thread the person never accepted, or accepted into a thread since discarded.
	OfferOpen OfferState = iota
	// OfferAccepted is a forge thread whose every comment is in the review thread as the forge holds it.
	OfferAccepted
	// OfferChanged is a forge thread with a comment written or edited since the person accepted it.
	OfferChanged
)

// StateOfOffer is where a forge thread stands against the review thread it was accepted into.
func StateOfOffer(threads map[string]*agentv1.ReviewThread, offer *pullrequestv1.ForgeThread) OfferState {
	if offer.GetAcceptedThreadId() == "" {
		return OfferOpen
	}

	accepted, ok := threads[offer.GetAcceptedThreadId()]
	if !ok {
		return OfferOpen
	}

	for _, comment := range offer.GetComments() {
		if !isCopied(accepted, comment) {
			return OfferChanged
		}
	}

	return OfferAccepted
}

func isCopied(thread *agentv1.ReviewThread, comment *pullrequestv1.ForgeComment) bool {
	return slices.ContainsFunc(thread.GetMessages(), func(message *agentv1.ReviewMessage) bool {
		forge := message.GetForge()

		return forge.GetCommentId() == comment.GetId() && forge.GetEditedAt() == comment.GetEditedAt()
	})
}

func isCountedOffer(threads map[string]*agentv1.ReviewThread, thread *pullrequestv1.ForgeThread) bool {
	comments := thread.GetComments()

	return thread.GetPath() != "" && !thread.GetResolved() && !thread.GetDismissed() && len(comments) > 0 && !comments[0].GetYours() && !isBotOffer(thread) &&
		StateOfOffer(threads, thread) != OfferAccepted
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

func setOfferCounts(row *agentv1.AgentRow, threads map[string]*agentv1.ReviewThread, offers []*pullrequestv1.ForgeThread) {
	external := 0
	for _, offer := range offers {
		if isCountedOffer(threads, offer) {
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
