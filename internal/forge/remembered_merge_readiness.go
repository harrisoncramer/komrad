package forge

import "time"

// mergeReadinessField is one part of a pull request's merge readiness whose move is reported.
type mergeReadinessField string

const (
	// MergeReadinessFieldDecision is where the reviews stand.
	MergeReadinessFieldDecision mergeReadinessField = "review"
	// MergeReadinessFieldChecks is where the checks on the head stand.
	MergeReadinessFieldChecks mergeReadinessField = "checks"
	// MergeReadinessFieldMergeability is where the branch stands against its base.
	MergeReadinessFieldMergeability mergeReadinessField = "mergeability"
	// MergeReadinessFieldAutoMerge is whether the forge will merge it on its own once its checks and reviews pass.
	MergeReadinessFieldAutoMerge mergeReadinessField = "auto-merge"
)

const (
	// AutoMergeOn is auto-merge turned on, in the words a move carries.
	AutoMergeOn  = "on"
	autoMergeOff = "off"
)

// mergeReadinessMove is one part of a pull request's merge readiness going from one value to another.
type mergeReadinessMove struct {
	Field mergeReadinessField
	From  string
	To    string
}

// RememberedAnswer is what komrad remembers of one part of a pull request's merge readiness.
type RememberedAnswer[T comparable] struct {
	// Value is the last answer the forge gave that was not blank.
	Value T
	// HeadSHA is the head Value was read for, empty for a part that belongs to the whole pull request.
	HeadSHA string
	// WasLatestReadBlank says the latest read answered blank.
	WasLatestReadBlank bool
}

// RememberedMergeReadiness is what komrad remembers of a pull request's merge readiness across its reads.
type RememberedMergeReadiness struct {
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

// RememberRead takes a read of head made at a moment into what komrad remembers, leaving the checks to their rows.
func (rs RememberedMergeReadiness) RememberRead(read MergeReadiness, head string, at time.Time) RememberedMergeReadiness {
	next := RememberedMergeReadiness{
		Decision:          rememberAnswer(rs.Decision, read.Decision, ""),
		Checks:            rs.Checks,
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

// ListMovesSince lists every part of the merge readiness that moved since before.
func (rs RememberedMergeReadiness) ListMovesSince(before RememberedMergeReadiness) []mergeReadinessMove {
	var moves []mergeReadinessMove
	if rs.Decision.Value != before.Decision.Value || isBackAfterBlank(before.Decision, rs.Decision) {
		moves = append(moves, mergeReadinessMove{
			Field: MergeReadinessFieldDecision,
			From:  string(before.Decision.Value),
			To:    string(rs.Decision.Value),
		})
	}
	if !rs.Checks.WasLatestReadBlank && (rs.Checks.Value != before.Checks.Value || isOnNewHead(before.Checks, rs.Checks)) {
		moves = append(moves, mergeReadinessMove{
			Field: MergeReadinessFieldChecks,
			From:  string(before.Checks.Value),
			To:    string(rs.Checks.Value),
		})
	}
	if !rs.Mergeability.WasLatestReadBlank && (rs.Mergeability.Value != before.Mergeability.Value || isOnNewHead(before.Mergeability, rs.Mergeability)) {
		moves = append(moves, mergeReadinessMove{
			Field: MergeReadinessFieldMergeability,
			From:  string(before.Mergeability.Value),
			To:    string(rs.Mergeability.Value),
		})
	}
	if from, to := DescribeAutoMerge(before.AutoMerge), DescribeAutoMerge(rs.AutoMerge); from != to {
		moves = append(moves, mergeReadinessMove{
			Field: MergeReadinessFieldAutoMerge,
			From:  from,
			To:    to,
		})
	}

	return moves
}

// isOnNewHead says whether an answer was read for a different head than before.
func isOnNewHead[T comparable](before, after RememberedAnswer[T]) bool {
	return before.HeadSHA != "" && after.HeadSHA != "" && before.HeadSHA != after.HeadSHA
}

// isBackAfterBlank says whether the decision was read again after a blank read.
func isBackAfterBlank(before, after RememberedAnswer[ReviewDecision]) bool {
	return before.WasLatestReadBlank && !after.WasLatestReadBlank
}

// DescribeAutoMerge says whether auto-merge is on in the words a move carries, empty before it was read.
func DescribeAutoMerge(autoMerge *AutoMerge) string {
	switch {
	case autoMerge == nil:
		return ""
	case autoMerge.On:
		return AutoMergeOn
	default:
		return autoMergeOff
	}
}

// displayedAnswer is one part of a pull request's merge readiness as komrad shows it for a head.
type displayedAnswer[T comparable] struct {
	// Value is the answer to show for the head.
	Value T
	// IsUnconfirmed says Value is shown through a blank read.
	IsUnconfirmed bool
}

// DisplayedMergeReadiness is a pull request's merge readiness as komrad shows it for one head.
type DisplayedMergeReadiness struct {
	Decision     displayedAnswer[ReviewDecision]
	Checks       displayedAnswer[Checks]
	Mergeability displayedAnswer[Mergeability]
	AutoMerge    AutoMerge
}

// DisplayForHead says what to show for each part of the merge readiness on head.
func (rs RememberedMergeReadiness) DisplayForHead(head string) DisplayedMergeReadiness {
	displayed := DisplayedMergeReadiness{
		Decision:     displayLatestDecision(rs.Decision),
		Checks:       displayAnswerForHead(rs.Checks, head),
		Mergeability: displayAnswerForHead(rs.Mergeability, head),
	}
	if rs.AutoMerge != nil {
		displayed.AutoMerge = *rs.AutoMerge
	}

	return displayed
}

func displayLatestDecision(answer RememberedAnswer[ReviewDecision]) displayedAnswer[ReviewDecision] {
	if answer.WasLatestReadBlank {
		return displayedAnswer[ReviewDecision]{}
	}

	return displayedAnswer[ReviewDecision]{Value: answer.Value}
}

func displayAnswerForHead[T comparable](answer RememberedAnswer[T], head string) displayedAnswer[T] {
	var blank T
	if answer.Value == blank || head != "" && answer.HeadSHA != head {
		return displayedAnswer[T]{}
	}

	return displayedAnswer[T]{Value: answer.Value, IsUnconfirmed: answer.WasLatestReadBlank}
}
