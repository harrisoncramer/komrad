package pullrequests

import (
	"context"
	"fmt"

	"komrad/internal/fail"
)

// ErrDismissHumanOnly is answered when an agent asks to dismiss a forge comment from the person's review.
var ErrDismissHumanOnly = fail.Denied("only the person reviewing can dismiss a comment from the forge")

type threadDismisser interface {
	SetDismissed(ctx context.Context, id string, dismissed bool) error
}

type NewDismisserParams struct {
	Threads threadDismisser
}

// Dismisser marks the forge's comments dismissed in the person's review.
type Dismisser struct {
	threads threadDismisser
}

// NewDismisser builds a dismisser over the forge thread store.
func NewDismisser(params NewDismisserParams) *Dismisser {
	return &Dismisser{threads: params.Threads}
}

// DismissForgeThreadParams names the forge thread to dismiss and who is asking.
type DismissForgeThreadParams struct {
	ID string
	// Restore makes the thread active again.
	Restore bool
	// AgentID is the agent asking, empty for the person.
	AgentID string
}

// DismissForgeThread marks a forge thread dismissed for the person, or active again.
func (d *Dismisser) DismissForgeThread(ctx context.Context, params DismissForgeThreadParams) error {
	if params.AgentID != "" {
		return fmt.Errorf("%w: agent %s", ErrDismissHumanOnly, params.AgentID)
	}

	return d.threads.SetDismissed(ctx, params.ID, !params.Restore)
}
