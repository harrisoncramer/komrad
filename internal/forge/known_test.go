package forge

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type read struct {
	standing Standing
	head     string
	at       time.Time
}

func foldAll(reads []read) (Known, Known) {
	var before, after Known
	for _, r := range reads {
		before, after = after, after.Fold(r.standing, r.head, r.at)
	}

	return before, after
}

// TestKnownFold covers which answers a read keeps and which head each is tied to.
func TestKnownFold(t *testing.T) {
	t.Parallel()

	began := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	later := began.Add(time.Minute)
	failing := Standing{Decision: ReviewDecisionPending, Checks: ChecksFailing, Mergeability: MergeabilityUnstable}

	tests := []struct {
		name  string
		reads []read
		want  Known
	}{
		{
			name:  "an answer is remembered with the head it was read for",
			reads: []read{{standing: failing, head: "aaa", at: began}},
			want: Known{
				Decision:     Last[ReviewDecision]{Value: ReviewDecisionPending},
				Checks:       Last[Checks]{Value: ChecksFailing, SHA: "aaa"},
				Mergeability: Last[Mergeability]{Value: MergeabilityUnstable, SHA: "aaa"},
				AutoMerge:    &AutoMerge{},
				ReadSHA:      "aaa",
			},
		},
		{
			name: "a blank keeps the last answer and its head",
			reads: []read{
				{standing: failing, head: "aaa", at: began},
				{head: "bbb", at: later},
			},
			want: Known{
				Decision:     Last[ReviewDecision]{Value: ReviewDecisionPending, Blank: true},
				Checks:       Last[Checks]{Value: ChecksFailing, SHA: "aaa", Blank: true},
				Mergeability: Last[Mergeability]{Value: MergeabilityUnstable, SHA: "aaa", Blank: true},
				AutoMerge:    &AutoMerge{},
				ReadSHA:      "bbb",
				BlankSince:   &later,
			},
		},
		{
			name: "an answer after a blank is no longer blank",
			reads: []read{
				{standing: failing, head: "aaa", at: began},
				{head: "aaa", at: later},
				{standing: Standing{Decision: ReviewDecisionApproved, Checks: ChecksPassing, Mergeability: MergeabilityClean}, head: "aaa", at: later},
			},
			want: Known{
				Decision:     Last[ReviewDecision]{Value: ReviewDecisionApproved},
				Checks:       Last[Checks]{Value: ChecksPassing, SHA: "aaa"},
				Mergeability: Last[Mergeability]{Value: MergeabilityClean, SHA: "aaa"},
				AutoMerge:    &AutoMerge{},
				ReadSHA:      "aaa",
			},
		},
		{
			name: "auto-merge is taken from every read",
			reads: []read{
				{standing: Standing{AutoMerge: AutoMerge{On: true, Allowed: true}}, head: "aaa", at: began},
				{head: "aaa", at: later},
			},
			want: Known{
				Decision:     Last[ReviewDecision]{Blank: true},
				Checks:       Last[Checks]{Blank: true},
				Mergeability: Last[Mergeability]{Blank: true},
				AutoMerge:    &AutoMerge{},
				ReadSHA:      "aaa",
				BlankSince:   &began,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, got := foldAll(tt.reads)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestKnownFoldBlankSince covers the blank clock starting only on a new blank, never on every read.
func TestKnownFoldBlankSince(t *testing.T) {
	t.Parallel()

	began := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	later := began.Add(time.Minute)
	clean := Standing{Mergeability: MergeabilityClean}

	tests := []struct {
		name  string
		reads []read
		want  *time.Time
	}{
		{
			name:  "a first blank begins the clock",
			reads: []read{{head: "aaa", at: began}},
			want:  &began,
		},
		{
			name: "a blank that stays blank on the same head keeps when it began",
			reads: []read{
				{standing: clean, head: "aaa", at: began.Add(-time.Minute)},
				{head: "aaa", at: began},
				{head: "aaa", at: later},
				{head: "aaa", at: later.Add(time.Minute)},
			},
			want: &began,
		},
		{
			name: "a blank on a new head begins again",
			reads: []read{
				{head: "aaa", at: began},
				{head: "bbb", at: later},
			},
			want: &later,
		},
		{
			name: "an answer clears the clock",
			reads: []read{
				{head: "aaa", at: began},
				{standing: clean, head: "aaa", at: later},
			},
		},
		{
			name: "a blank after an answer on the same head begins again",
			reads: []read{
				{head: "aaa", at: began},
				{standing: clean, head: "aaa", at: began.Add(time.Second)},
				{head: "aaa", at: later},
			},
			want: &later,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, got := foldAll(tt.reads)
			assert.Equal(t, tt.want, got.BlankSince)
		})
	}
}

// TestKnownMoves covers what counts as news between two folds.
func TestKnownMoves(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	failing := Standing{Decision: ReviewDecisionPending, Checks: ChecksFailing, Mergeability: MergeabilityClean}

	tests := []struct {
		name  string
		reads []read
		want  []Move
	}{
		{
			name:  "a first answer moves from nothing",
			reads: []read{{standing: failing, head: "aaa", at: at}},
			want: []Move{
				{Field: FieldDecision, To: "pending"},
				{Field: FieldChecks, To: "failing"},
				{Field: FieldMergeability, To: "clean"},
				{Field: FieldAutoMerge, To: "off"},
			},
		},
		{
			name: "a blank is never a move",
			reads: []read{
				{standing: failing, head: "aaa", at: at},
				{head: "aaa", at: at},
			},
		},
		{
			name: "a blank on a new head is never a move",
			reads: []read{
				{standing: failing, head: "aaa", at: at},
				{head: "bbb", at: at},
			},
		},
		{
			name: "the same answer again is not a move",
			reads: []read{
				{standing: failing, head: "aaa", at: at},
				{standing: failing, head: "aaa", at: at},
			},
		},
		{
			name: "the same checks verdict on a new head is a move",
			reads: []read{
				{standing: failing, head: "aaa", at: at},
				{standing: failing, head: "bbb", at: at},
			},
			want: []Move{{Field: FieldChecks, From: "failing", To: "failing"}},
		},
		{
			name: "the same checks verdict after a blank on a new head is a move",
			reads: []read{
				{standing: failing, head: "aaa", at: at},
				{head: "bbb", at: at},
				{standing: failing, head: "bbb", at: at},
			},
			want: []Move{{Field: FieldChecks, From: "failing", To: "failing"}},
		},
		{
			name: "a changed answer moves from the last one",
			reads: []read{
				{standing: failing, head: "aaa", at: at},
				{standing: Standing{
					Decision:     ReviewDecisionApproved,
					Checks:       ChecksPassing,
					Mergeability: MergeabilityConflicting,
					AutoMerge:    AutoMerge{On: true},
				}, head: "aaa", at: at},
			},
			want: []Move{
				{Field: FieldDecision, From: "pending", To: "approved"},
				{Field: FieldChecks, From: "failing", To: "passing"},
				{Field: FieldMergeability, From: "clean", To: "conflicting"},
				{Field: FieldAutoMerge, From: "off", To: "on"},
			},
		},
		{
			name: "auto-merge becoming allowed is not a move",
			reads: []read{
				{standing: failing, head: "aaa", at: at},
				{standing: Standing{
					Decision:     ReviewDecisionPending,
					Checks:       ChecksFailing,
					Mergeability: MergeabilityClean,
					AutoMerge:    AutoMerge{Allowed: true},
				}, head: "aaa", at: at},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			before, after := foldAll(tt.reads)
			assert.Equal(t, tt.want, after.Moves(before))
		})
	}
}

// TestKnownAt covers which answers hold for a head and which are only remembered.
func TestKnownAt(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	failing := Standing{
		Decision:     ReviewDecisionApproved,
		Checks:       ChecksFailing,
		Mergeability: MergeabilityBehind,
		AutoMerge:    AutoMerge{On: true, Allowed: true},
	}

	tests := []struct {
		name  string
		reads []read
		head  string
		want  View
	}{
		{
			name: "nothing read shows nothing",
			head: "aaa",
		},
		{
			name:  "an answer for the head is confirmed",
			reads: []read{{standing: failing, head: "aaa", at: at}},
			head:  "aaa",
			want: View{
				Decision:     Shown[ReviewDecision]{Value: ReviewDecisionApproved},
				Checks:       Shown[Checks]{Value: ChecksFailing, SHA: "aaa"},
				Mergeability: Shown[Mergeability]{Value: MergeabilityBehind, SHA: "aaa"},
				AutoMerge:    AutoMerge{On: true, Allowed: true},
			},
		},
		{
			name: "the last answer through a blank read is unconfirmed",
			reads: []read{
				{standing: failing, head: "aaa", at: at},
				{head: "aaa", at: at},
			},
			head: "aaa",
			want: View{
				Decision:     Shown[ReviewDecision]{Value: ReviewDecisionApproved, Unconfirmed: true},
				Checks:       Shown[Checks]{Value: ChecksFailing, SHA: "aaa", Unconfirmed: true},
				Mergeability: Shown[Mergeability]{Value: MergeabilityBehind, SHA: "aaa", Unconfirmed: true},
			},
		},
		{
			name: "checks and mergeability from an older head are stale while the decision holds",
			reads: []read{
				{standing: failing, head: "aaa", at: at},
				{head: "bbb", at: at},
			},
			head: "bbb",
			want: View{
				Decision:     Shown[ReviewDecision]{Value: ReviewDecisionApproved, Unconfirmed: true},
				Checks:       Shown[Checks]{Value: ChecksFailing, SHA: "aaa", Stale: true},
				Mergeability: Shown[Mergeability]{Value: MergeabilityBehind, SHA: "aaa", Stale: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, known := foldAll(tt.reads)
			assert.Equal(t, tt.want, known.At(tt.head))
		})
	}
}

// TestShownNow covers a stale answer showing as blank.
func TestShownNow(t *testing.T) {
	t.Parallel()

	assert.Equal(t, ChecksFailing, Shown[Checks]{Value: ChecksFailing, SHA: "aaa"}.Now())
	assert.Equal(t, ChecksFailing, Shown[Checks]{Value: ChecksFailing, SHA: "aaa", Unconfirmed: true}.Now())
	assert.Equal(t, ChecksNone, Shown[Checks]{Value: ChecksFailing, SHA: "aaa", Stale: true}.Now())
}
