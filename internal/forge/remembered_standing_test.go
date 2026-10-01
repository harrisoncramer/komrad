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

func rememberEveryRead(reads []read) (RememberedStanding, RememberedStanding) {
	var before, after RememberedStanding
	for _, next := range reads {
		before, after = after, after.RememberRead(next.standing, next.head, next.at)
	}

	return before, after
}

// TestRememberRead covers which answers a read keeps and which head each is tied to.
func TestRememberRead(t *testing.T) {
	t.Parallel()

	began := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	later := began.Add(time.Minute)
	failing := Standing{
		Decision:     ReviewDecisionPending,
		Checks:       ChecksFailing,
		Mergeability: MergeabilityUnstable,
	}

	tests := []struct {
		name  string
		reads []read
		want  RememberedStanding
	}{
		{
			name: "an answer is remembered with the head it was read for",
			reads: []read{{
				standing: failing,
				head:     "aaa",
				at:       began,
			}},
			want: RememberedStanding{
				Decision:          RememberedAnswer[ReviewDecision]{Value: ReviewDecisionPending},
				Checks:            RememberedAnswer[Checks]{Value: ChecksFailing, HeadSHA: "aaa"},
				Mergeability:      RememberedAnswer[Mergeability]{Value: MergeabilityUnstable, HeadSHA: "aaa"},
				AutoMerge:         &AutoMerge{},
				LatestReadHeadSHA: "aaa",
			},
		},
		{
			name: "a blank keeps the last answer and its head",
			reads: []read{
				{
					standing: failing,
					head:     "aaa",
					at:       began,
				},
				{head: "bbb", at: later},
			},
			want: RememberedStanding{
				Decision: RememberedAnswer[ReviewDecision]{Value: ReviewDecisionPending, WasLatestReadBlank: true},
				Checks: RememberedAnswer[Checks]{
					Value:              ChecksFailing,
					HeadSHA:            "aaa",
					WasLatestReadBlank: true,
				},
				Mergeability: RememberedAnswer[Mergeability]{
					Value:              MergeabilityUnstable,
					HeadSHA:            "aaa",
					WasLatestReadBlank: true,
				},
				AutoMerge:              &AutoMerge{},
				LatestReadHeadSHA:      "bbb",
				MergeabilityBlankSince: &later,
			},
		},
		{
			name: "an answer after a blank is no longer blank",
			reads: []read{
				{
					standing: failing,
					head:     "aaa",
					at:       began,
				},
				{head: "aaa", at: later},
				{
					standing: Standing{
						Decision:     ReviewDecisionApproved,
						Checks:       ChecksPassing,
						Mergeability: MergeabilityClean,
					},
					head: "aaa",
					at:   later,
				},
			},
			want: RememberedStanding{
				Decision:          RememberedAnswer[ReviewDecision]{Value: ReviewDecisionApproved},
				Checks:            RememberedAnswer[Checks]{Value: ChecksPassing, HeadSHA: "aaa"},
				Mergeability:      RememberedAnswer[Mergeability]{Value: MergeabilityClean, HeadSHA: "aaa"},
				AutoMerge:         &AutoMerge{},
				LatestReadHeadSHA: "aaa",
			},
		},
		{
			name: "auto-merge is taken from every read",
			reads: []read{
				{
					standing: Standing{AutoMerge: AutoMerge{On: true, Allowed: true}},
					head:     "aaa",
					at:       began,
				},
				{head: "aaa", at: later},
			},
			want: RememberedStanding{
				Decision:               RememberedAnswer[ReviewDecision]{WasLatestReadBlank: true},
				Checks:                 RememberedAnswer[Checks]{WasLatestReadBlank: true},
				Mergeability:           RememberedAnswer[Mergeability]{WasLatestReadBlank: true},
				AutoMerge:              &AutoMerge{},
				LatestReadHeadSHA:      "aaa",
				MergeabilityBlankSince: &began,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, got := rememberEveryRead(tt.reads)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestRememberReadMergeabilityBlankSince covers the blank clock starting only on a new blank, never on every read.
func TestRememberReadMergeabilityBlankSince(t *testing.T) {
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
				{
					standing: clean,
					head:     "aaa",
					at:       began.Add(-time.Minute),
				},
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
				{
					standing: clean,
					head:     "aaa",
					at:       later,
				},
			},
		},
		{
			name: "a blank after an answer on the same head begins again",
			reads: []read{
				{head: "aaa", at: began},
				{
					standing: clean,
					head:     "aaa",
					at:       began.Add(time.Second),
				},
				{head: "aaa", at: later},
			},
			want: &later,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, got := rememberEveryRead(tt.reads)
			assert.Equal(t, tt.want, got.MergeabilityBlankSince)
		})
	}
}

// TestListMovesSince covers what counts as news between two reads.
func TestListMovesSince(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	failing := Standing{
		Decision:     ReviewDecisionPending,
		Checks:       ChecksFailing,
		Mergeability: MergeabilityClean,
	}

	tests := []struct {
		name  string
		reads []read
		want  []StandingMove
	}{
		{
			name: "a first answer moves from nothing",
			reads: []read{{
				standing: failing,
				head:     "aaa",
				at:       at,
			}},
			want: []StandingMove{
				{Field: StandingFieldDecision, To: "pending"},
				{Field: StandingFieldChecks, To: "failing"},
				{Field: StandingFieldMergeability, To: "clean"},
				{Field: StandingFieldAutoMerge, To: "off"},
			},
		},
		{
			name: "a blank is never a move",
			reads: []read{
				{
					standing: failing,
					head:     "aaa",
					at:       at,
				},
				{head: "aaa", at: at},
			},
		},
		{
			name: "a blank on a new head is never a move",
			reads: []read{
				{
					standing: failing,
					head:     "aaa",
					at:       at,
				},
				{head: "bbb", at: at},
			},
		},
		{
			name: "the same answer again is not a move",
			reads: []read{
				{
					standing: failing,
					head:     "aaa",
					at:       at,
				},
				{
					standing: failing,
					head:     "aaa",
					at:       at,
				},
			},
		},
		{
			name: "the same checks verdict on a new head is a move",
			reads: []read{
				{
					standing: failing,
					head:     "aaa",
					at:       at,
				},
				{
					standing: failing,
					head:     "bbb",
					at:       at,
				},
			},
			want: []StandingMove{{
				Field: StandingFieldChecks,
				From:  "failing",
				To:    "failing",
			}},
		},
		{
			name: "the same checks verdict after a blank on a new head is a move",
			reads: []read{
				{
					standing: failing,
					head:     "aaa",
					at:       at,
				},
				{head: "bbb", at: at},
				{
					standing: failing,
					head:     "bbb",
					at:       at,
				},
			},
			want: []StandingMove{{
				Field: StandingFieldChecks,
				From:  "failing",
				To:    "failing",
			}},
		},
		{
			name: "a changed answer moves from the last one",
			reads: []read{
				{
					standing: failing,
					head:     "aaa",
					at:       at,
				},
				{standing: Standing{
					Decision:     ReviewDecisionApproved,
					Checks:       ChecksPassing,
					Mergeability: MergeabilityConflicting,
					AutoMerge:    AutoMerge{On: true},
				}, head: "aaa", at: at},
			},
			want: []StandingMove{
				{
					Field: StandingFieldDecision,
					From:  "pending",
					To:    "approved",
				},
				{
					Field: StandingFieldChecks,
					From:  "failing",
					To:    "passing",
				},
				{
					Field: StandingFieldMergeability,
					From:  "clean",
					To:    "conflicting",
				},
				{
					Field: StandingFieldAutoMerge,
					From:  "off",
					To:    "on",
				},
			},
		},
		{
			name: "auto-merge becoming allowed is not a move",
			reads: []read{
				{
					standing: failing,
					head:     "aaa",
					at:       at,
				},
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

			before, after := rememberEveryRead(tt.reads)
			assert.Equal(t, tt.want, after.ListMovesSince(before))
		})
	}
}

// TestDisplayForHead covers which answers hold for a head and which are only remembered.
func TestDisplayForHead(t *testing.T) {
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
		want  DisplayedStanding
	}{
		{
			name: "nothing read shows nothing",
			head: "aaa",
		},
		{
			name: "an answer for the head is confirmed",
			reads: []read{{
				standing: failing,
				head:     "aaa",
				at:       at,
			}},
			head: "aaa",
			want: DisplayedStanding{
				Decision:     DisplayedAnswer[ReviewDecision]{Value: ReviewDecisionApproved},
				Checks:       DisplayedAnswer[Checks]{Value: ChecksFailing},
				Mergeability: DisplayedAnswer[Mergeability]{Value: MergeabilityBehind},
				AutoMerge:    AutoMerge{On: true, Allowed: true},
			},
		},
		{
			name: "a blank read leaves checks and mergeability unconfirmed and the decision none",
			reads: []read{
				{
					standing: failing,
					head:     "aaa",
					at:       at,
				},
				{head: "aaa", at: at},
			},
			head: "aaa",
			want: DisplayedStanding{
				Checks:       DisplayedAnswer[Checks]{Value: ChecksFailing, IsUnconfirmed: true},
				Mergeability: DisplayedAnswer[Mergeability]{Value: MergeabilityBehind, IsUnconfirmed: true},
			},
		},
		{
			name: "a blank read on a new head shows nothing",
			reads: []read{
				{
					standing: failing,
					head:     "aaa",
					at:       at,
				},
				{head: "bbb", at: at},
			},
			head: "bbb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, remembered := rememberEveryRead(tt.reads)
			assert.Equal(t, tt.want, remembered.DisplayForHead(tt.head))
		})
	}
}
