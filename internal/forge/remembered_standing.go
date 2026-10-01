package forge

import "time"

// StandingField is one part of a pull request's standing whose move is reported.
type StandingField string

const (
	// StandingFieldDecision is where the reviews stand.
	StandingFieldDecision StandingField = "review"
	// StandingFieldChecks is where the checks on the head stand.
	StandingFieldChecks StandingField = "checks"
	// StandingFieldMergeability is where the branch stands against its base.
	StandingFieldMergeability StandingField = "mergeability"
	// StandingFieldAutoMerge is whether the forge will merge it on its own once its checks and reviews pass.
	StandingFieldAutoMerge StandingField = "auto-merge"
)

const (
	autoMergeOn  = "on"
	autoMergeOff = "off"
)

// StandingMove is one part of a pull request's standing going from one value to another.
type StandingMove struct {
	Field StandingField
	From  string
	To    string
}

// RememberedAnswer is what komrad remembers of one part of a pull request's standing.
type RememberedAnswer[T comparable] struct {
	// Value is the last answer the forge gave that was not blank.
	Value T
	// HeadSHA is the head Value was read for, empty for a part that belongs to the whole pull request.
	HeadSHA string
	// WasLatestReadBlank says the latest read answered blank.
	WasLatestReadBlank bool
}

// RememberedStanding is what komrad remembers of a pull request's standing across its reads.
type RememberedStanding struct {
	Decision     RememberedAnswer[ReviewDecision]
	Checks       RememberedAnswer[Checks]
	Mergeability RememberedAnswer[Mergeability]
	// AutoMerge is the latest auto-merge read, nil before the first.
	AutoMerge *AutoMerge
	// LatestReadHeadSHA is the head the latest read was for.
	LatestReadHeadSHA string
	// MergeabilityBlankSince is when the forge began answering a blank mergeability for LatestReadHeadSHA.
	MergeabilityBlankSince *time.Time
}

// RememberRead takes a read of head made at a moment into what komrad remembers.
func (rs RememberedStanding) RememberRead(read Standing, head string, at time.Time) RememberedStanding {
	next := RememberedStanding{
		Decision:          rememberAnswer(rs.Decision, read.Decision, ""),
		Checks:            rememberAnswer(rs.Checks, read.Checks, head),
		Mergeability:      rememberAnswer(rs.Mergeability, read.Mergeability, head),
		AutoMerge:         &read.AutoMerge,
		LatestReadHeadSHA: head,
	}

	switch {
	case read.Mergeability != MergeabilityUnknown:
	case rs.MergeabilityBlankSince != nil && rs.LatestReadHeadSHA == head:
		next.MergeabilityBlankSince = rs.MergeabilityBlankSince
	default:
		next.MergeabilityBlankSince = &at
	}

	return next
}

func rememberAnswer[T comparable](answer RememberedAnswer[T], read T, head string) RememberedAnswer[T] {
	var blank T
	if read == blank {
		answer.WasLatestReadBlank = true

		return answer
	}

	return RememberedAnswer[T]{Value: read, HeadSHA: head}
}

// ListMovesSince lists every part of the standing that moved since before.
func (rs RememberedStanding) ListMovesSince(before RememberedStanding) []StandingMove {
	var moves []StandingMove
	if rs.Decision.Value != before.Decision.Value {
		moves = append(moves, StandingMove{
			Field: StandingFieldDecision,
			From:  string(before.Decision.Value),
			To:    string(rs.Decision.Value),
		})
	}
	if rs.Checks.Value != before.Checks.Value || isSameVerdictOnNewHead(before.Checks, rs.Checks) {
		moves = append(moves, StandingMove{
			Field: StandingFieldChecks,
			From:  string(before.Checks.Value),
			To:    string(rs.Checks.Value),
		})
	}
	if rs.Mergeability.Value != before.Mergeability.Value {
		moves = append(moves, StandingMove{
			Field: StandingFieldMergeability,
			From:  string(before.Mergeability.Value),
			To:    string(rs.Mergeability.Value),
		})
	}
	if from, to := describeAutoMerge(before.AutoMerge), describeAutoMerge(rs.AutoMerge); from != to {
		moves = append(moves, StandingMove{
			Field: StandingFieldAutoMerge,
			From:  from,
			To:    to,
		})
	}

	return moves
}

// isSameVerdictOnNewHead says whether the checks gave the same verdict again on a different head.
func isSameVerdictOnNewHead(before, after RememberedAnswer[Checks]) bool {
	return before.HeadSHA != "" && after.HeadSHA != "" && before.HeadSHA != after.HeadSHA
}

// describeAutoMerge says whether auto-merge is on in the words a move carries.
func describeAutoMerge(autoMerge *AutoMerge) string {
	switch {
	case autoMerge == nil:
		return ""
	case autoMerge.On:
		return autoMergeOn
	default:
		return autoMergeOff
	}
}

// DisplayedAnswer is one part of a pull request's standing as komrad shows it for a head.
type DisplayedAnswer[T comparable] struct {
	// Value is the answer to show for the head.
	Value T
	// IsUnconfirmed says Value is shown through a blank read.
	IsUnconfirmed bool
}

// DisplayedStanding is a pull request's standing as komrad shows it for one head.
type DisplayedStanding struct {
	Decision     DisplayedAnswer[ReviewDecision]
	Checks       DisplayedAnswer[Checks]
	Mergeability DisplayedAnswer[Mergeability]
	AutoMerge    AutoMerge
}

// DisplayForHead says what to show for each part of the standing on head.
func (rs RememberedStanding) DisplayForHead(head string) DisplayedStanding {
	displayed := DisplayedStanding{
		Decision:     displayAnswerForHead(rs.Decision, ""),
		Checks:       displayAnswerForHead(rs.Checks, head),
		Mergeability: displayAnswerForHead(rs.Mergeability, head),
	}
	if rs.AutoMerge != nil {
		displayed.AutoMerge = *rs.AutoMerge
	}

	return displayed
}

func displayAnswerForHead[T comparable](answer RememberedAnswer[T], head string) DisplayedAnswer[T] {
	var blank T
	if answer.Value == blank || head != "" && answer.HeadSHA != head {
		return DisplayedAnswer[T]{}
	}

	return DisplayedAnswer[T]{Value: answer.Value, IsUnconfirmed: answer.WasLatestReadBlank}
}
