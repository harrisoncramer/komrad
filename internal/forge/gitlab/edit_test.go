package gitlab

import (
	"testing"

	"komrad/internal/forge"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const editAddress = "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/7"

// editTargetAnswer is the read of a merge request's text, with whether the user may change it.
func editTargetAnswer(canUpdate bool) answer {
	permission := "false"
	if canUpdate {
		permission = "true"
	}

	return answer{body: `{
	"data": {
		"project": {
			"mergeRequest": {
				"title": "fix the sync",
				"description": "The sync read twice.",
				"userPermissions": {
					"updateMergeRequest": ` + permission + `
				}
			}
		}
	}
}`}
}

// TestReadText covers reading a merge request's text and refusing one the credential may not change.
func TestReadText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		answers  map[string]answer
		wantText *forge.Text
		wantErr  error
	}{
		{
			name:     "a merge request the credential may change",
			answers:  map[string]answer{"EditTarget": editTargetAnswer(true)},
			wantText: &forge.Text{Title: "fix the sync", Body: "The sync read twice."},
		},
		{
			name:    "a merge request the credential may not change",
			answers: map[string]answer{"EditTarget": editTargetAnswer(false)},
			wantErr: forge.ErrNotEditable,
		},
		{
			name: "a merge request gitlab does not have",
			answers: map[string]answer{"EditTarget": {body: `{
	"data": {
		"project": {
			"mergeRequest": null
		}
	}
}`}},
			wantErr: forge.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, _ := serve(t, tt.answers)

			text, err := f.ReadText(t.Context(), editAddress)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantText, text)
		})
	}
}

// TestEdit covers sending only the fields asked for, sending nothing that changes nothing, and
// GitLab's refusals.
func TestEdit(t *testing.T) {
	t.Parallel()

	read := forge.Text{Title: "fix the sync", Body: "The sync read twice."}
	updated := answer{body: `{
	"data": {
		"mergeRequestUpdate": {
			"errors": []
		}
	}
}`}

	tests := []struct {
		name          string
		params        forge.EditParams
		answers       map[string]answer
		wantErr       error
		wantVariables map[string]any
	}{
		{
			name:          "a new title alone",
			params:        forge.EditParams{URL: editAddress, EditedFrom: read, Title: xptr.Ptr("fix(sync): read once")},
			answers:       map[string]answer{"EditTarget": editTargetAnswer(true), "UpdateMergeRequest": updated},
			wantVariables: map[string]any{"path": "harrisoncramer/komrad", "iid": "7", "title": "fix(sync): read once"},
		},
		{
			name:          "a new description alone",
			params:        forge.EditParams{URL: editAddress, EditedFrom: read, Body: xptr.Ptr("It reads once.")},
			answers:       map[string]answer{"EditTarget": editTargetAnswer(true), "UpdateMergeRequest": updated},
			wantVariables: map[string]any{"path": "harrisoncramer/komrad", "iid": "7", "description": "It reads once."},
		},
		{
			name:    "the text gitlab already holds",
			params:  forge.EditParams{URL: editAddress, EditedFrom: read, Body: xptr.Ptr(read.Body)},
			answers: map[string]answer{"EditTarget": editTargetAnswer(true)},
		},
		{
			name:    "a merge request the credential may not change",
			params:  forge.EditParams{URL: editAddress, EditedFrom: read, Title: xptr.Ptr("fix(sync): read once")},
			answers: map[string]answer{"EditTarget": editTargetAnswer(false)},
			wantErr: forge.ErrNotEditable,
		},
		{
			name:   "a title gitlab will not take",
			params: forge.EditParams{URL: editAddress, EditedFrom: read, Title: xptr.Ptr("fix(sync): read once")},
			answers: map[string]answer{
				"EditTarget": editTargetAnswer(true),
				"UpdateMergeRequest": {body: `{
	"data": {
		"mergeRequestUpdate": {
			"errors": [
				"Title is too long"
			]
		}
	}
}`},
			},
			wantErr:       forge.ErrNotEditable,
			wantVariables: map[string]any{"path": "harrisoncramer/komrad", "iid": "7", "title": "fix(sync): read once"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, tt.answers)

			err := f.Edit(t.Context(), tt.params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			sent := xslice.Filter(fake.calls, func(c call) bool { return c.Operation == "UpdateMergeRequest" })
			if tt.wantVariables == nil {
				assert.Empty(t, sent)

				return
			}
			require.Len(t, sent, 1)
			assert.Equal(t, tt.wantVariables, sent[0].Variables)
		})
	}
}
