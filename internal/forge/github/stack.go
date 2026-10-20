package github

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
	"komrad/lib/xhttp"
)

var _ forge.Stacker = (*Forge)(nil)

// stackNode is a GitHub stack as the REST API answers it.
type stackNode struct {
	Number       int `json:"number"`
	PullRequests []struct {
		Number   int        `json:"number"`
		State    string     `json:"state"`
		MergedAt *time.Time `json:"merged_at"`
	} `json:"pull_requests"`
}

// Stack puts pull requests, bottom first, in a new GitHub stack, or on top of the stack numbered.
func (f *Forge) Stack(ctx context.Context, params forge.StackParams) error {
	if !f.stacks {
		return forge.ErrNotStackable.Saying("%s has no GitHub stacks", f.client.API())
	}

	owner, name, err := ownerAndName(params.Repo.Path)
	if err != nil {
		return err
	}

	address := fmt.Sprintf("%s/repos/%s/%s/stacks", f.client.API(), owner, name)
	if params.Stack != 0 {
		address = fmt.Sprintf("%s/%d/add", address, params.Stack)
	}

	answer, err := f.client.Send(ctx, xhttp.JSONParams{
		Method: http.MethodPost,
		URL:    address,
		Body:   map[string][]int{"pull_requests": params.Numbers},
	}, nil)
	if isRefusal(answer, err) {
		return forge.ErrNotStackable.Saying("github will not stack %v in %s/%s: %s", params.Numbers, owner, name, refusalMessage(answer, err))
	}
	if err != nil {
		return fmt.Errorf("failed to stack %v in %s/%s: %w", params.Numbers, owner, name, err)
	}

	return nil
}

// Retarget takes the pull request at the web address out of its GitHub stack, along with every other
// open one in it, and changes its base branch.
func (f *Forge) Retarget(ctx context.Context, address, base string) error {
	owner, name, number, err := parsePullURL(address)
	if err != nil {
		return err
	}

	stacks, err := f.listStacks(ctx, owner, name, number)
	if err != nil {
		return err
	}

	for _, stack := range stacks {
		answer, err := f.client.Send(ctx, xhttp.JSONParams{
			Method: http.MethodPost,
			URL:    fmt.Sprintf("%s/repos/%s/%s/stacks/%d/unstack", f.client.API(), owner, name, stack.Number),
		}, nil)
		if isRefusal(answer, err) {
			return forge.ErrNotStackable.Saying("github will not take #%d out of its stack: %s", number, refusalMessage(answer, err))
		}
		if err != nil {
			return fmt.Errorf("failed to take #%d out of its stack: %w", number, err)
		}
	}

	answer, err := f.client.Send(ctx, xhttp.JSONParams{
		Method: http.MethodPatch,
		URL:    fmt.Sprintf("%s/repos/%s/%s/pulls/%d", f.client.API(), owner, name, number),
		Body:   map[string]string{"base": base},
	}, nil)
	if answer.Status == http.StatusNotFound {
		return noPullRequest(number, owner, name)
	}
	if isRefusal(answer, err) {
		return forge.ErrNotStackable.Saying("github will not change the base branch of #%d to %s: %s", number, base, refusalMessage(answer, err))
	}
	if err != nil {
		return fmt.Errorf("failed to change the base branch of #%d to %s: %w", number, base, err)
	}

	return nil
}

// listStacks is the GitHub stacks holding a pull request, of which there is at most one.
func (f *Forge) listStacks(ctx context.Context, owner, name string, number int) ([]stackNode, error) {
	var stacks []stackNode
	answer, err := f.client.Send(ctx, xhttp.JSONParams{
		Method: http.MethodGet,
		URL:    fmt.Sprintf("%s/repos/%s/%s/stacks?pull_request=%d", f.client.API(), owner, name, number),
	}, &stacks)
	if err != nil {
		return nil, fmt.Errorf("failed to read the stack of #%d: %w", number, err)
	}
	if answer.Status == http.StatusNotFound {
		return nil, nil
	}

	return stacks, nil
}

// isRefusal says whether GitHub turned a REST call down for what it asked, rather than for the
// credential or the rate limit.
func isRefusal(answer xhttp.Answer, err error) bool {
	if answer.Status == http.StatusNotFound {
		return true
	}

	return err != nil && fail.Of(err) == nil && answer.IsRefusal()
}

// refusalMessage is GitHub's reason for turning a REST call down.
func refusalMessage(answer xhttp.Answer, err error) string {
	if err == nil {
		return "github has no such stack or pull request"
	}

	return graphql.MessageOf(answer.Body)
}
