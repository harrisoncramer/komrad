package forge

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type read struct {
	readiness MergeReadiness
	checks    RememberedAnswer[Checks]
	head      string
	at        time.Time
}

func rememberEveryRead(reads []read) (RememberedMergeReadiness, RememberedMergeReadiness) {
	var before, after RememberedMergeReadiness
	for _, next := range reads {
		before, after = after, after.RememberRead(next.readiness, next.head, next.at)
		after.Checks = next.checks
	}

	return before, after
}

// TestRememberRead covers which answers a read keeps and which head each is tied to.
func TestRememberRead(t *testing.T) {
	t.Parallel()

	began := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	later := began.Add(time.Minute)
	failing := MergeReadiness{
		Decision:     ReviewDecisionPending,
		Mergeability: MergeabilityUnstable,
	}

	tests := []struct {
		name  string
		reads []read
		want  RememberedMergeReadiness
	}{
		{
			name: "an answer is remembered with the head it was read for",
			reads: []read{{
				readiness: failing,
				head:      "aaa",
				at:        began,
			}},
			want: RememberedMergeReadiness{
				Decision:          RememberedAnswer[ReviewDecision]{Value: ReviewDecisionPending},
				Mergeability:      RememberedAnswer[Mergeability]{Value: MergeabilityUnstable, HeadSHA: "aaa"},
				AutoMerge:         &AutoMerge{},
				LatestReadHeadSHA: "aaa",
			},
		},
		{
			name: "a blank keeps the last answer and its head",
			reads: []read{
				{
					readiness: failing,
					head:      "aaa",
					at:        began,
				},
				{head: "bbb", at: later},
			},
			want: RememberedMergeReadiness{
				Decision: RememberedAnswer[ReviewDecision]{Value: ReviewDecisionPending, WasLatestReadBlank: true},
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
					readiness: failing,
					head:      "aaa",
					at:        began,
				},
				{head: "aaa", at: later},
				{
					readiness: MergeReadiness{
						Decision:     ReviewDecisionApproved,
						Mergeability: MergeabilityClean,
					},
					head: "aaa",
					at:   later,
				},
			},
			want: RememberedMergeReadiness{
				Decision:          RememberedAnswer[ReviewDecision]{Value: ReviewDecisionApproved},
				Mergeability:      RememberedAnswer[Mergeability]{Value: MergeabilityClean, HeadSHA: "aaa"},
				AutoMerge:         &AutoMerge{},
				LatestReadHeadSHA: "aaa",
			},
		},
		{
			name: "auto-merge is taken from every read",
			reads: []read{
				{
					readiness: MergeReadiness{AutoMerge: AutoMerge{On: true, Allowed: true}},
					head:      "aaa",
					at:        began,
				},
				{head: "aaa", at: later},
			},
			want: RememberedMergeReadiness{
				Decision:               RememberedAnswer[ReviewDecision]{WasLatestReadBlank: true},
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
	clean := MergeReadiness{Mergeability: MergeabilityClean}

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
					readiness: clean,
					head:      "aaa",
					at:        began.Add(-time.Minute),
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
					readiness: clean,
					head:      "aaa",
					at:        later,
				},
			},
		},
		{
			name: "a blank after an answer on the same head begins again",
			reads: []read{
				{head: "aaa", at: began},
				{
					readiness: clean,
					head:      "aaa",
					at:        began.Add(time.Second),
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
	failing := MergeReadiness{
		Decision:     ReviewDecisionPending,
		Mergeability: MergeabilityClean,
	}

	tests := []struct {
		name  string
		reads []read
		want  []mergeReadinessMove
	}{
		{
			name: "a first answer moves from nothing",
			reads: []read{{
				readiness: failing,
				checks:    RememberedAnswer[Checks]{Value: ChecksFailing, HeadSHA: "aaa"},
				head:      "aaa",
				at:        at,
			}},
			want: []mergeReadinessMove{
				{Field: MergeReadinessFieldDecision, To: "pending"},
				{Field: MergeReadinessFieldChecks, To: "failing"},
				{Field: MergeReadinessFieldMergeability, To: "clean"},
				{Field: MergeReadinessFieldAutoMerge, To: "off"},
			},
		},
		{
			name: "a blank is never a move",
			reads: []read{
				{
					readiness: failing,
					checks:    RememberedAnswer[Checks]{Value: ChecksFailing, HeadSHA: "aaa"},
					head:      "aaa",
					at:        at,
				},
				{
					checks: RememberedAnswer[Checks]{
						Value:              ChecksFailing,
						HeadSHA:            "aaa",
						WasLatestReadBlank: true,
					},
					head: "aaa",
					at:   at,
				},
			},
		},
		{
			name: "a blank on a new head is never a move",
			reads: []read{
				{
					readiness: failing,
					checks:    RememberedAnswer[Checks]{Value: ChecksFailing, HeadSHA: "aaa"},
					head:      "aaa",
					at:        at,
				},
				{
					checks: RememberedAnswer[Checks]{HeadSHA: "bbb", WasLatestReadBlank: true},
					head:   "bbb",
					at:     at,
				},
			},
		},
		{
			name: "the same answer again is not a move",
			reads: []read{
				{
					readiness: failing,
					checks:    RememberedAnswer[Checks]{Value: ChecksFailing, HeadSHA: "aaa"},
					head:      "aaa",
					at:        at,
				},
				{
					readiness: failing,
					checks:    RememberedAnswer[Checks]{Value: ChecksFailing, HeadSHA: "aaa"},
					head:      "aaa",
					at:        at,
				},
			},
		},
		{
			name: "the same answers on a new head are moves",
			reads: []read{
				{
					readiness: failing,
					checks:    RememberedAnswer[Checks]{Value: ChecksFailing, HeadSHA: "aaa"},
					head:      "aaa",
					at:        at,
				},
				{
					readiness: failing,
					checks:    RememberedAnswer[Checks]{Value: ChecksFailing, HeadSHA: "bbb"},
					head:      "bbb",
					at:        at,
				},
			},
			want: []mergeReadinessMove{
				{
					Field: MergeReadinessFieldChecks,
					From:  "failing",
					To:    "failing",
				},
				{
					Field: MergeReadinessFieldMergeability,
					From:  "clean",
					To:    "clean",
				},
			},
		},
		{
			name: "answers read after a blank on a new head are moves",
			reads: []read{
				{
					readiness: failing,
					checks:    RememberedAnswer[Checks]{Value: ChecksFailing, HeadSHA: "aaa"},
					head:      "aaa",
					at:        at,
				},
				{
					readiness: MergeReadiness{Decision: ReviewDecisionPending},
					checks:    RememberedAnswer[Checks]{HeadSHA: "bbb", WasLatestReadBlank: true},
					head:      "bbb",
					at:        at,
				},
				{
					readiness: failing,
					checks:    RememberedAnswer[Checks]{Value: ChecksFailing, HeadSHA: "bbb"},
					head:      "bbb",
					at:        at,
				},
			},
			want: []mergeReadinessMove{
				{
					Field: MergeReadinessFieldChecks,
					From:  "",
					To:    "failing",
				},
				{
					Field: MergeReadinessFieldMergeability,
					From:  "clean",
					To:    "clean",
				},
			},
		},
		{
			name: "the same review decision after a blank is a move",
			reads: []read{
				{
					readiness: MergeReadiness{Decision: ReviewDecisionChangesRequested},
					head:      "aaa",
					at:        at,
				},
				{head: "aaa", at: at},
				{
					readiness: MergeReadiness{Decision: ReviewDecisionChangesRequested},
					head:      "aaa",
					at:        at,
				},
			},
			want: []mergeReadinessMove{{
				Field: MergeReadinessFieldDecision,
				From:  "changes_requested",
				To:    "changes_requested",
			}},
		},
		{
			name: "a changed answer moves from the last one",
			reads: []read{
				{
					readiness: failing,
					checks:    RememberedAnswer[Checks]{Value: ChecksFailing, HeadSHA: "aaa"},
					head:      "aaa",
					at:        at,
				},
				{readiness: MergeReadiness{
					Decision:     ReviewDecisionApproved,
					Mergeability: MergeabilityConflicting,
					AutoMerge:    AutoMerge{On: true},
				}, checks: RememberedAnswer[Checks]{Value: ChecksPassing, HeadSHA: "aaa"}, head: "aaa", at: at},
			},
			want: []mergeReadinessMove{
				{
					Field: MergeReadinessFieldDecision,
					From:  "pending",
					To:    "approved",
				},
				{
					Field: MergeReadinessFieldChecks,
					From:  "failing",
					To:    "passing",
				},
				{
					Field: MergeReadinessFieldMergeability,
					From:  "clean",
					To:    "conflicting",
				},
				{
					Field: MergeReadinessFieldAutoMerge,
					From:  "off",
					To:    "on",
				},
			},
		},
		{
			name: "auto-merge becoming allowed is not a move",
			reads: []read{
				{
					readiness: failing,
					checks:    RememberedAnswer[Checks]{Value: ChecksFailing, HeadSHA: "aaa"},
					head:      "aaa",
					at:        at,
				},
				{readiness: MergeReadiness{
					Decision:     ReviewDecisionPending,
					Mergeability: MergeabilityClean,
					AutoMerge:    AutoMerge{Allowed: true},
				}, checks: RememberedAnswer[Checks]{Value: ChecksFailing, HeadSHA: "aaa"}, head: "aaa", at: at},
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
	failing := MergeReadiness{
		Decision:     ReviewDecisionApproved,
		Mergeability: MergeabilityBehind,
		AutoMerge:    AutoMerge{On: true, Allowed: true},
	}

	tests := []struct {
		name  string
		reads []read
		head  string
		want  DisplayedMergeReadiness
	}{
		{
			name: "nothing read shows nothing",
			head: "aaa",
		},
		{
			name: "an answer for the head is confirmed",
			reads: []read{{
				readiness: failing,
				checks:    RememberedAnswer[Checks]{Value: ChecksFailing, HeadSHA: "aaa"},
				head:      "aaa",
				at:        at,
			}},
			head: "aaa",
			want: DisplayedMergeReadiness{
				Decision:     displayedAnswer[ReviewDecision]{Value: ReviewDecisionApproved},
				Checks:       displayedAnswer[Checks]{Value: ChecksFailing},
				Mergeability: displayedAnswer[Mergeability]{Value: MergeabilityBehind},
				AutoMerge:    AutoMerge{On: true, Allowed: true},
			},
		},
		{
			name: "a blank read leaves checks and mergeability unconfirmed and the decision none",
			reads: []read{
				{
					readiness: failing,
					checks:    RememberedAnswer[Checks]{Value: ChecksFailing, HeadSHA: "aaa"},
					head:      "aaa",
					at:        at,
				},
				{
					checks: RememberedAnswer[Checks]{
						Value:              ChecksFailing,
						HeadSHA:            "aaa",
						WasLatestReadBlank: true,
					},
					head: "aaa",
					at:   at,
				},
			},
			head: "aaa",
			want: DisplayedMergeReadiness{
				Checks:       displayedAnswer[Checks]{Value: ChecksFailing, IsUnconfirmed: true},
				Mergeability: displayedAnswer[Mergeability]{Value: MergeabilityBehind, IsUnconfirmed: true},
			},
		},
		{
			name: "a blank read on a new head shows nothing",
			reads: []read{
				{
					readiness: failing,
					checks:    RememberedAnswer[Checks]{Value: ChecksFailing, HeadSHA: "aaa"},
					head:      "aaa",
					at:        at,
				},
				{
					checks: RememberedAnswer[Checks]{HeadSHA: "bbb", WasLatestReadBlank: true},
					head:   "bbb",
					at:     at,
				},
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
