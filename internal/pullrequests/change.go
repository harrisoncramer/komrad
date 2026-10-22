package pullrequests

import (
	"komrad/internal/eventprompts"

	"github.com/google/uuid"
)

const changeTopic = "pull_request_changed"

// field is one part of a pull request whose change is reported.
type field = eventprompts.PullRequestFieldName

const (
	fieldState        = eventprompts.PullRequestFieldState
	fieldDecision     = eventprompts.PullRequestFieldDecision
	fieldChecks       = eventprompts.PullRequestFieldChecks
	fieldMergeability = eventprompts.PullRequestFieldMergeability
	fieldAutoMerge    = eventprompts.PullRequestFieldAutoMerge
)

var fieldOrder = []field{fieldState, fieldDecision, fieldChecks, fieldMergeability, fieldAutoMerge}

var alarming = map[field][]string{
	fieldDecision:     {"changes_requested"},
	fieldChecks:       {"failing", "failing_running"},
	fieldMergeability: {"conflicting", "behind"},
}

// Move is one field of a pull request going from one value to another.
type Move struct {
	Field field  `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// Change is the fields of one pull request that moved in one write.
type Change struct {
	ID        uuid.UUID  `json:"id"`
	Moves     []Move     `json:"moves"`
	Recipient *uuid.UUID `json:"recipient,omitempty"`
}

// Topic names the handler that tells the agents on the branch.
func (Change) Topic() string {
	return changeTopic
}

// moved is the move between two values of a field, nil when they are the same or either is unknown.
func moved(field field, from, to *string) *Move {
	if from == nil || to == nil || *from == *to {
		return nil
	}

	return &Move{
		Field: field,
		From:  *from,
		To:    *to,
	}
}

// newChange is the change for the fields of a pull request that moved or a check that newly failed, nil when
// neither happened.
func newChange(id uuid.UUID, moves []Move, newFailure bool) *Change {
	if len(moves) == 0 && !newFailure {
		return nil
	}

	return &Change{ID: id, Moves: moves}
}
