package pullrequests

import (
	"context"
	"errors"
	"slices"
	"time"

	"komrad/internal/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/xslice"
)

// stackCall is one call that builds a GitHub stack: the pull requests to stack and the stack they go on.
type stackCall struct {
	// stack is the number of the stack to add to, zero to create one.
	stack   int
	members []pullrequeststore.StackingPullRequest
}

// stackShift is an open pull request in a GitHub stack whose base branch moved, which GitHub follows by
// rebasing it and every pull request over it.
type stackShift struct {
	repositoryURL string
	place         forge.StackPlace
}

// buildStacks brings the GitHub stacks of one repository in line with komrad's stacks, and heats the
// host when it changed any.
func (s *PullRequestSyncer) buildStacks(ctx context.Context, host *hostRead, repository candidate) error {
	stacker, ok := host.forge.(forge.Stacker)
	if !ok {
		return nil
	}

	pullRequests, err := s.pullRequests.ListStacking(ctx, repository.repositoryURL)
	if err != nil {
		return err
	}

	retargeted, err := s.retargetOntoBase(ctx, stacker, pullRequests)
	if err != nil {
		return err
	}
	if retargeted {
		s.Heat(host.host)

		return nil
	}

	built := false
	for _, run := range findRuns(pullRequests) {
		call := planStack(run)
		if call == nil || isEveryStackingRefused(call.members) {
			continue
		}

		err := stacker.Stack(ctx, forge.StackParams{
			Repo:  repository.remote,
			Stack: call.stack,
			Numbers: xslice.Map(call.members, func(member pullrequeststore.StackingPullRequest) int {
				return member.Number
			}),
		})
		if errors.Is(err, forge.ErrNotStackable) {
			ids := xslice.Map(call.members, func(member pullrequeststore.StackingPullRequest) string {
				return member.ID
			})
			if err := s.pullRequests.RecordStackingRefused(ctx, ids); err != nil {
				return err
			}

			continue
		}
		if err != nil {
			return err
		}

		built = true
	}
	if built {
		s.Heat(host.host)
	}

	return nil
}

// retargetOntoBase takes each pull request whose task was re-targeted onto the base branch out of its
// GitHub stack and points it there, and reports whether it moved any.
func (s *PullRequestSyncer) retargetOntoBase(ctx context.Context, stacker forge.Stacker, pullRequests []pullrequeststore.StackingPullRequest) (bool, error) {
	retargeted := false
	for _, pullRequest := range pullRequests {
		if pullRequest.RetargetOnto == "" || pullRequest.IsStackingRefused() {
			continue
		}
		if pullRequest.Stack == nil {
			if err := s.pullRequests.ClearRetarget(ctx, pullRequest.ID); err != nil {
				return false, err
			}

			continue
		}

		err := stacker.Retarget(ctx, pullRequest.URL, pullRequest.RetargetOnto)
		if errors.Is(err, forge.ErrNotStackable) {
			if err := s.pullRequests.RecordStackingRefused(ctx, []string{pullRequest.ID}); err != nil {
				return false, err
			}

			continue
		}
		if err != nil {
			return false, err
		}
		if err := s.pullRequests.ClearRetarget(ctx, pullRequest.ID); err != nil {
			return false, err
		}

		retargeted = true
	}

	return retargeted, nil
}

// findRuns is, from each task without a parent, the longest run of open pull requests where each
// targets the branch of the one under it, leaving out any with auto-merge on.
func findRuns(pullRequests []pullrequeststore.StackingPullRequest) [][]pullrequeststore.StackingPullRequest {
	children := map[string][]pullrequeststore.StackingPullRequest{}
	var bottoms []pullrequeststore.StackingPullRequest
	for _, pullRequest := range pullRequests {
		if pullRequest.AutoMerge {
			continue
		}
		if pullRequest.ParentTaskID == "" {
			bottoms = append(bottoms, pullRequest)

			continue
		}
		children[pullRequest.ParentTaskID] = append(children[pullRequest.ParentTaskID], pullRequest)
	}

	runs := make([][]pullrequeststore.StackingPullRequest, 0, len(bottoms))
	for _, bottom := range bottoms {
		run := []pullrequeststore.StackingPullRequest{bottom}
		for {
			next, ok := pickNext(run[len(run)-1], children[run[len(run)-1].TaskID])
			if !ok {
				break
			}
			run = append(run, next)
		}
		runs = append(runs, run)
	}

	return runs
}

// pickNext is the child pull request that goes on top of under: one targeting its branch, the one
// already in under's GitHub stack first, then the oldest.
func pickNext(under pullrequeststore.StackingPullRequest, children []pullrequeststore.StackingPullRequest) (pullrequeststore.StackingPullRequest, bool) {
	var picked pullrequeststore.StackingPullRequest
	found := false
	for _, child := range children {
		if child.BaseBranch != under.HeadBranch {
			continue
		}
		if isSameStack(under, child) {
			return child, true
		}
		if !found {
			picked, found = child, true
		}
	}

	return picked, found
}

// isSameStack says whether two pull requests sit in one GitHub stack.
func isSameStack(a, b pullrequeststore.StackingPullRequest) bool {
	return a.Stack != nil && b.Stack != nil && a.Stack.Number == b.Stack.Number
}

// planStack is the call that brings a run into one GitHub stack, nil when the run needs none or is
// stacked some other way.
func planStack(run []pullrequeststore.StackingPullRequest) *stackCall {
	if len(run) < 2 {
		return nil
	}
	if run[0].Stack == nil {
		if isAnyInGitHubStack(run) {
			return nil
		}

		return &stackCall{members: run}
	}

	bottom := *run[0].Stack
	stacked := 1
	for stacked < len(run) {
		place := run[stacked].Stack
		if place == nil || place.Number != bottom.Number || place.Position != bottom.Position+stacked {
			break
		}
		stacked++
	}

	rest := run[stacked:]
	if len(rest) == 0 || !run[stacked-1].Stack.IsTop() {
		return nil
	}
	if isAnyInGitHubStack(rest) {
		return nil
	}

	return &stackCall{stack: bottom.Number, members: rest}
}

// isEveryStackingRefused says whether GitHub refused to stack each pull request as it stands.
func isEveryStackingRefused(members []pullrequeststore.StackingPullRequest) bool {
	for _, member := range members {
		if !member.IsStackingRefused() {
			return false
		}
	}

	return true
}

// isAnyInGitHubStack says whether any of the pull requests already sits in a GitHub stack.
func isAnyInGitHubStack(members []pullrequeststore.StackingPullRequest) bool {
	return slices.ContainsFunc(members, func(member pullrequeststore.StackingPullRequest) bool {
		return member.Stack != nil
	})
}

// awaitRebasesParams is the shifts one read found, each pull request's head before the read, and when it was read.
type awaitRebasesParams struct {
	shifts     []stackShift
	priorHeads map[string]string
	readAt     time.Time
}

// awaitRebases records, for each shift, that komrad waits on GitHub to rebase the pull request and
// every open one over it in its GitHub stack, each from the head it had before this read.
func awaitRebases(ctx context.Context, st *pullrequeststore.PullRequestStore, params awaitRebasesParams) error {
	for _, shift := range params.shifts {
		members, err := st.ListGitHubStack(ctx, pullrequeststore.ListGitHubStackParams{
			RepositoryURL: shift.repositoryURL,
			Number:        shift.place.Number,
		})
		if err != nil {
			return err
		}

		for _, member := range members {
			if member.Position < shift.place.Position {
				continue
			}

			from, ok := params.priorHeads[member.ID]
			if !ok {
				from = member.HeadSHA
			}
			if err := st.RecordRebaseAwait(ctx, pullrequeststore.RecordRebaseAwaitParams{
				ID:      member.ID,
				Since:   params.readAt,
				HeadSHA: from,
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

// shiftOf is the shift an open pull request in a GitHub stack makes when its base branch moved, nil
// otherwise.
func shiftOf(pullRequest forge.PullRequest, oldBase *string) *stackShift {
	if pullRequest.Stack == nil || pullRequest.State != "OPEN" || oldBase == nil || *oldBase == pullRequest.BaseBranch {
		return nil
	}

	return &stackShift{repositoryURL: pullRequest.Repository.Canonical(), place: *pullRequest.Stack}
}
