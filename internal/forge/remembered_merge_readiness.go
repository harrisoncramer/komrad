package forge

import (
	"time"

	"komrad/lib/protoenum"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"google.golang.org/protobuf/reflect/protoreflect"
)

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
	// Value is the last answer the forge gave that was not blank, unspecified before the first.
	Value T
	// HeadSHA is the head Value was read for, empty for a part that belongs to the whole pull request.
	HeadSHA string
	// WasLatestReadBlank says the latest read answered blank.
	WasLatestReadBlank bool
}

// RememberedMergeReadiness is what komrad remembers of a pull request's merge readiness across its reads.
type RememberedMergeReadiness struct {
	Decision     RememberedAnswer[pullrequestv1.ReviewDecision]
	Checks       RememberedAnswer[pullrequestv1.Checks]
	Mergeability RememberedAnswer[pullrequestv1.Mergeability]
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
		Decision:          rememberAnswer(rs.Decision, read.Decision, pullrequestv1.ReviewDecision_REVIEW_DECISION_NONE, ""),
		Checks:            rs.Checks,
		Mergeability:      rememberAnswer(rs.Mergeability, read.Mergeability, pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN, head),
		AutoMerge:         &read.AutoMerge,
		LatestReadHeadSHA: head,
	}

	switch {
	case !isBlank(read.Mergeability, pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN):
	case rs.MergeabilityBlankSince != nil && rs.LatestReadHeadSHA == head:
		next.MergeabilityBlankSince = rs.MergeabilityBlankSince
	default:
		next.MergeabilityBlankSince = &at
	}

	return next
}

func rememberAnswer[T comparable](answer RememberedAnswer[T], read, blank T, head string) RememberedAnswer[T] {
	if isBlank(read, blank) {
		answer.WasLatestReadBlank = true

		return answer
	}

	return RememberedAnswer[T]{Value: read, HeadSHA: head}
}

// isBlank says whether the forge gave no answer, either unspecified or the blank it reports.
func isBlank[T comparable](value, blank T) bool {
	var unspecified T

	return value == unspecified || value == blank
}

// ListMovesSince lists every part of the merge readiness that moved since before.
func (rs RememberedMergeReadiness) ListMovesSince(before RememberedMergeReadiness) []mergeReadinessMove {
	var moves []mergeReadinessMove
	if rs.Decision.Value != before.Decision.Value || isBackAfterBlank(before.Decision, rs.Decision) {
		moves = append(moves, mergeReadinessMove{
			Field: MergeReadinessFieldDecision,
			From:  DescribeDecision(before.Decision.Value),
			To:    DescribeDecision(rs.Decision.Value),
		})
	}
	if !rs.Checks.WasLatestReadBlank && (rs.Checks.Value != before.Checks.Value || isOnNewHead(before.Checks, rs.Checks)) {
		moves = append(moves, mergeReadinessMove{
			Field: MergeReadinessFieldChecks,
			From:  DescribeChecks(before.Checks.Value),
			To:    DescribeChecks(rs.Checks.Value),
		})
	}
	if !rs.Mergeability.WasLatestReadBlank && (rs.Mergeability.Value != before.Mergeability.Value || isOnNewHead(before.Mergeability, rs.Mergeability)) {
		moves = append(moves, mergeReadinessMove{
			Field: MergeReadinessFieldMergeability,
			From:  DescribeMergeability(before.Mergeability.Value),
			To:    DescribeMergeability(rs.Mergeability.Value),
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
func isBackAfterBlank(before, after RememberedAnswer[pullrequestv1.ReviewDecision]) bool {
	return before.WasLatestReadBlank && !after.WasLatestReadBlank
}

// DescribeDecision is the review decision in the words a move carries, empty when the forge said nothing.
func DescribeDecision(decision pullrequestv1.ReviewDecision) string {
	return describeAnswer(decision, pullrequestv1.ReviewDecision_REVIEW_DECISION_NONE)
}

// DescribeChecks is where the checks stand in the words a move carries, empty when the forge reports none.
func DescribeChecks(checks pullrequestv1.Checks) string {
	return describeAnswer(checks, pullrequestv1.Checks_CHECKS_NONE)
}

// DescribeMergeability is the mergeability in the words a move carries, empty while the forge has not computed it.
func DescribeMergeability(mergeability pullrequestv1.Mergeability) string {
	return describeAnswer(mergeability, pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN)
}

func describeAnswer[T interface {
	comparable
	protoreflect.Enum
}](value, blank T) string {
	if isBlank(value, blank) {
		return ""
	}

	return protoenum.Name(value)
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
	Decision     displayedAnswer[pullrequestv1.ReviewDecision]
	Checks       displayedAnswer[pullrequestv1.Checks]
	Mergeability displayedAnswer[pullrequestv1.Mergeability]
	AutoMerge    AutoMerge
}

// DisplayForHead says what to show for each part of the merge readiness on head.
func (rs RememberedMergeReadiness) DisplayForHead(head string) DisplayedMergeReadiness {
	displayed := DisplayedMergeReadiness{
		Decision:     displayLatestDecision(rs.Decision),
		Checks:       displayAnswerForHead(rs.Checks, pullrequestv1.Checks_CHECKS_NONE, head),
		Mergeability: displayAnswerForHead(rs.Mergeability, pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN, head),
	}
	if rs.AutoMerge != nil {
		displayed.AutoMerge = *rs.AutoMerge
	}

	return displayed
}

func displayLatestDecision(answer RememberedAnswer[pullrequestv1.ReviewDecision]) displayedAnswer[pullrequestv1.ReviewDecision] {
	if answer.WasLatestReadBlank || isBlank(answer.Value, pullrequestv1.ReviewDecision_REVIEW_DECISION_NONE) {
		return displayedAnswer[pullrequestv1.ReviewDecision]{Value: pullrequestv1.ReviewDecision_REVIEW_DECISION_NONE}
	}

	return displayedAnswer[pullrequestv1.ReviewDecision]{Value: answer.Value}
}

func displayAnswerForHead[T comparable](answer RememberedAnswer[T], blank T, head string) displayedAnswer[T] {
	if isBlank(answer.Value, blank) || head != "" && answer.HeadSHA != head {
		return displayedAnswer[T]{Value: blank}
	}

	return displayedAnswer[T]{Value: answer.Value, IsUnconfirmed: answer.WasLatestReadBlank}
}
