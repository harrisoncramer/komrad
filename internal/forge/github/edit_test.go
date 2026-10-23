package github

import (
	"testing"

	"komrad/internal/forge"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const editAddress = "https://github.com/harrisoncramer/komrad/pull/742"

// editTargetAnswer is the read of a pull request's text, with whether the viewer may change it.
func editTargetAnswer(canUpdate bool) answer {
	return answer{body: data(`{
	"repository": {
		"pullRequest": {
			"id": "PR_742",
			"title": "fix the sync",
			"body": "The sync read twice.\r\nIt reads once.",
			"viewerCanUpdate": ` + boolOf(canUpdate) + `
		}
	}
}`)}
}

// TestReadText covers reading a pull request's text and refusing one the credential may not change.
func TestReadText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		answers  map[string]answer
		wantText *forge.Text
		wantErr  error
	}{
		{
			name:     "a pull request the credential may change, with the browser's line endings dropped",
			answers:  map[string]answer{"EditTarget": editTargetAnswer(true)},
			wantText: &forge.Text{Title: "fix the sync", Body: "The sync read twice.\nIt reads once."},
		},
		{
			name:    "a pull request the credential may not change",
			answers: map[string]answer{"EditTarget": editTargetAnswer(false)},
			wantErr: forge.ErrNotEditable,
		},
		{
			name: "a pull request github does not have",
			answers: map[string]answer{"EditTarget": {body: data(`{
	"repository": {
		"pullRequest": null
	}
}`)}},
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
// the forge's refusals.
func TestEdit(t *testing.T) {
	t.Parallel()

	read := forge.Text{Title: "fix the sync", Body: "The sync read twice.\nIt reads once."}
	updated := answer{body: data(`{
	"updatePullRequest": {
		"clientMutationId": null
	}
}`)}

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
			answers:       map[string]answer{"EditTarget": editTargetAnswer(true), "UpdatePullRequest": updated},
			wantVariables: map[string]any{"id": "PR_742", "title": "fix(sync): read once"},
		},
		{
			name:          "a new description alone",
			params:        forge.EditParams{URL: editAddress, EditedFrom: read, Body: xptr.Ptr("It reads once.")},
			answers:       map[string]answer{"EditTarget": editTargetAnswer(true), "UpdatePullRequest": updated},
			wantVariables: map[string]any{"id": "PR_742", "body": "It reads once."},
		},
		{
			name:    "the text github already holds",
			params:  forge.EditParams{URL: editAddress, EditedFrom: read, Title: xptr.Ptr(read.Title)},
			answers: map[string]answer{"EditTarget": editTargetAnswer(true)},
		},
		{
			name:    "a pull request the credential may not change",
			params:  forge.EditParams{URL: editAddress, EditedFrom: read, Title: xptr.Ptr("fix(sync): read once")},
			answers: map[string]answer{"EditTarget": editTargetAnswer(false)},
			wantErr: forge.ErrNotEditable,
		},
		{
			name:   "a title github will not take",
			params: forge.EditParams{URL: editAddress, EditedFrom: read, Title: xptr.Ptr("fix(sync): read once")},
			answers: map[string]answer{
				"EditTarget": editTargetAnswer(true),
				"UpdatePullRequest": {body: `{
	"data": {
		"updatePullRequest": null
	},
	"errors": [
		{
			"type": "UNPROCESSABLE",
			"path": [
				"updatePullRequest"
			],
			"message": "Title can't be blank"
		}
	]
}`},
			},
			wantErr:       forge.ErrNotEditable,
			wantVariables: map[string]any{"id": "PR_742", "title": "fix(sync): read once"},
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
			sent := xslice.Filter(fake.calls, func(c call) bool { return c.Operation == "UpdatePullRequest" })
			if tt.wantVariables == nil {
				assert.Empty(t, sent)

				return
			}
			require.Len(t, sent, 1)
			assert.Equal(t, tt.wantVariables, sent[0].Variables)
		})
	}
}
