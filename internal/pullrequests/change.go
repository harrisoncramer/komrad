package pullrequests

import (
	"slices"
)

const changeTopic = "pull_request_changed"

// Field is one part of a pull request whose change is reported.
type Field string

const (
	// FieldState is whether the pull request is open, merged or closed.
	FieldState Field = "state"
	// FieldDecision is where the reviews stand.
	FieldDecision Field = "review"
	// FieldChecks is where the checks on the head stand.
	FieldChecks Field = "checks"
	// FieldMergeability is where the branch stands against its base.
	FieldMergeability Field = "mergeability"
)

var fieldOrder = []Field{FieldState, FieldDecision, FieldChecks, FieldMergeability}

var opening = map[Field][]string{
	FieldState:        {"MERGED", "CLOSED"},
	FieldDecision:     {"approved", "changes_requested"},
	FieldChecks:       {"failing"},
	FieldMergeability: {"conflicting", "behind"},
}

// Move is one field of a pull request going from one value to another.
type Move struct {
	Field Field  `json:"field"`
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

// Opens says whether the change is news enough to start mail rather than only update it.
func (c Change) Opens() bool {
	return slices.ContainsFunc(c.Moves, func(move Move) bool {
		return slices.Contains(opening[move.Field], move.To)
	})
}

// Moved is the move between two known values of a field, nil when either is unknown or they
// are the same.
func Moved(field Field, from, to *string) *Move {
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
func NewChange(id string, moved ...*Move) *Change {
	var moves []Move
	for _, move := range moved {
		if move != nil {
			moves = append(moves, *move)
		}
	}
	if len(moves) == 0 {
		return nil
	}

	return &Change{ID: id, Moves: moves}
}
