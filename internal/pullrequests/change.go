package pullrequests

import (
	"slices"

	"komrad/internal/forge"
)

const changeTopic = "pull_request_changed"

// field is one part of a pull request whose change is reported.
type field string

const (
	// fieldState is whether the pull request is open, merged or closed.
	fieldState field = "state"
	// fieldDecision is where the reviews stand.
	fieldDecision = field(forge.MergeReadinessFieldDecision)
	// fieldChecks is where the checks on the head stand.
	fieldChecks = field(forge.MergeReadinessFieldChecks)
	// fieldMergeability is where the branch stands against its base.
	fieldMergeability = field(forge.MergeReadinessFieldMergeability)
	// fieldAutoMerge is whether the forge will merge it on its own once its checks and reviews pass.
	fieldAutoMerge = field(forge.MergeReadinessFieldAutoMerge)
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
	ID        string `json:"id"`
	Moves     []Move `json:"moves"`
	Recipient string `json:"recipient,omitempty"`
}

// Topic names the handler that tells the agents on the branch.
func (Change) Topic() string {
	return changeTopic
}

// opens says whether the change is news enough to start mail rather than only update it.
func (c Change) opens() bool {
	return slices.ContainsFunc(c.Moves, func(move Move) bool {
		_, ok := eventOf[move.Field][move.To]

		return ok
	})
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

// NewChange is the change for the fields of a pull request that moved, nil when none did.
func NewChange(id string, moves []Move) *Change {
	if len(moves) == 0 {
		return nil
	}

	return &Change{ID: id, Moves: moves}
}
