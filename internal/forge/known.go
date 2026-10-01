package forge

import "time"

// Field is one part of a pull request's standing whose move is reported.
type Field string

const (
	// FieldDecision is where the reviews stand.
	FieldDecision Field = "review"
	// FieldChecks is where the checks on the head stand.
	FieldChecks Field = "checks"
	// FieldMergeability is where the branch stands against its base.
	FieldMergeability Field = "mergeability"
	// FieldAutoMerge is whether the forge will merge it on its own once its checks and reviews pass.
	FieldAutoMerge Field = "auto-merge"
)

const (
	autoMergeOn  = "on"
	autoMergeOff = "off"
)

// Move is one part of a pull request's standing going from one value to another.
type Move struct {
	Field Field
	From  string
	To    string
}

// Last is what komrad remembers of one part of a pull request's standing.
type Last[T comparable] struct {
	// Value is the last answer the forge gave that was not blank.
	Value T
	// SHA is the head Value was read for, empty for a part that belongs to the whole pull request.
	SHA string
	// Blank says the latest read answered blank.
	Blank bool
}

// Known is what komrad remembers of a pull request's standing across its reads.
type Known struct {
	Decision     Last[ReviewDecision]
	Checks       Last[Checks]
	Mergeability Last[Mergeability]
	// AutoMerge is the latest auto-merge read, nil before the first.
	AutoMerge *AutoMerge
	// ReadSHA is the head the latest read was for.
	ReadSHA string
	// BlankSince is when the forge began answering a blank mergeability for ReadSHA.
	BlankSince *time.Time
}

// Fold is what komrad knows once a read of head at a moment is taken in.
func (k Known) Fold(read Standing, head string, at time.Time) Known {
	next := Known{
		Decision:     fold(k.Decision, read.Decision, ""),
		Checks:       fold(k.Checks, read.Checks, head),
		Mergeability: fold(k.Mergeability, read.Mergeability, head),
		AutoMerge:    &read.AutoMerge,
		ReadSHA:      head,
	}

	switch {
	case read.Mergeability != MergeabilityUnknown:
	case k.BlankSince != nil && k.ReadSHA == head:
		next.BlankSince = k.BlankSince
	default:
		next.BlankSince = &at
	}

	return next
}

func fold[T comparable](last Last[T], read T, head string) Last[T] {
	var blank T
	if read == blank {
		last.Blank = true

		return last
	}

	return Last[T]{Value: read, SHA: head}
}

// Moves is every part of the standing that moved since before.
func (k Known) Moves(before Known) []Move {
	var moves []Move
	if k.Decision.Value != before.Decision.Value {
		moves = append(moves, Move{Field: FieldDecision, From: string(before.Decision.Value), To: string(k.Decision.Value)})
	}
	if k.Checks.Value != before.Checks.Value || isRerun(before.Checks, k.Checks) {
		moves = append(moves, Move{Field: FieldChecks, From: string(before.Checks.Value), To: string(k.Checks.Value)})
	}
	if k.Mergeability.Value != before.Mergeability.Value {
		moves = append(moves, Move{Field: FieldMergeability, From: string(before.Mergeability.Value), To: string(k.Mergeability.Value)})
	}
	if from, to := autoMergeWord(before.AutoMerge), autoMergeWord(k.AutoMerge); from != to {
		moves = append(moves, Move{Field: FieldAutoMerge, From: from, To: to})
	}

	return moves
}

// isRerun says whether the checks gave the same verdict again on a different head.
func isRerun(before, after Last[Checks]) bool {
	return before.SHA != "" && after.SHA != "" && before.SHA != after.SHA
}

// autoMergeWord is whether auto-merge is on in the words a move carries, empty before it was read.
func autoMergeWord(autoMerge *AutoMerge) string {
	switch {
	case autoMerge == nil:
		return ""
	case autoMerge.On:
		return autoMergeOn
	default:
		return autoMergeOff
	}
}

// Shown is one part of a pull request's standing as komrad shows it for a head.
type Shown[T comparable] struct {
	// Value is the last answer the forge gave that was not blank.
	Value T
	// SHA is the head Value was read for, empty for a part that belongs to the whole pull request.
	SHA string
	// Stale says Value was read for another head.
	Stale bool
	// Unconfirmed says Value is shown through a blank read.
	Unconfirmed bool
}

// Now is the value that holds for the head, blank when the last answer was for another.
func (s Shown[T]) Now() T {
	if s.Stale {
		var blank T

		return blank
	}

	return s.Value
}

// View is a pull request's standing as komrad shows it for one head.
type View struct {
	Decision     Shown[ReviewDecision]
	Checks       Shown[Checks]
	Mergeability Shown[Mergeability]
	AutoMerge    AutoMerge
}

// At is the standing to show for head.
func (k Known) At(head string) View {
	view := View{
		Decision:     shown(k.Decision, ""),
		Checks:       shown(k.Checks, head),
		Mergeability: shown(k.Mergeability, head),
	}
	if k.AutoMerge != nil {
		view.AutoMerge = *k.AutoMerge
	}

	return view
}

func shown[T comparable](last Last[T], head string) Shown[T] {
	var blank T
	if last.Value == blank {
		return Shown[T]{}
	}

	stale := head != "" && last.SHA != head

	return Shown[T]{
		Value:       last.Value,
		SHA:         last.SHA,
		Stale:       stale,
		Unconfirmed: last.Blank && !stale,
	}
}
