package forge

import (
	"testing"

	"komrad/lib/xptr"
	forgev1 "komrad/proto/gen/forge/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEditParamsChanges covers an edit refused for the credential or for text that moved, and one
// that would change nothing.
func TestEditParamsChanges(t *testing.T) {
	t.Parallel()

	read := Text{Title: "fix the sync", Body: "The sync read twice."}
	target := TextTarget{
		Forge:   forgev1.Kind_KIND_GITHUB,
		Ref:     "#7",
		Text:    read,
		CanEdit: true,
	}

	tests := []struct {
		name       string
		target     TextTarget
		params     EditParams
		wantErr    error
		wantChange bool
	}{
		{
			name: "a credential the forge will not let change the text",
			target: TextTarget{
				Forge: forgev1.Kind_KIND_GITHUB,
				Ref:   "#7",
				Text:  read,
			},
			params:  EditParams{EditedFrom: read, Title: xptr.Ptr("fix(sync): read once")},
			wantErr: ErrNotEditable,
		},
		{
			name:   "a title that changed on the forge since it was read",
			target: target,
			params: EditParams{
				EditedFrom: Text{Title: "wip", Body: read.Body},
				Title:      xptr.Ptr("fix(sync): read once"),
			},
			wantErr: ErrTextMoved,
		},
		{
			name:   "a description that changed on the forge since it was read",
			target: target,
			params: EditParams{
				EditedFrom: Text{Title: read.Title},
				Body:       xptr.Ptr("The sync reads once."),
			},
			wantErr: ErrTextMoved,
		},
		{
			name:   "the text the forge already holds",
			target: target,
			params: EditParams{
				EditedFrom: read,
				Title:      xptr.Ptr(read.Title),
				Body:       xptr.Ptr(read.Body),
			},
		},
		{
			name: "an edit the forge already holds, made again",
			target: TextTarget{
				Forge:   forgev1.Kind_KIND_GITHUB,
				Ref:     "#7",
				Text:    Text{Title: "fix(sync): read once", Body: read.Body},
				CanEdit: true,
			},
			params: EditParams{EditedFrom: read, Title: xptr.Ptr("fix(sync): read once")},
		},
		{
			name:       "a new title from the text on the forge",
			target:     target,
			params:     EditParams{EditedFrom: read, Title: xptr.Ptr("fix(sync): read once")},
			wantChange: true,
		},
		{
			name:       "a new description from the text on the forge",
			target:     target,
			params:     EditParams{EditedFrom: read, Body: xptr.Ptr("The sync reads once.")},
			wantChange: true,
		},
		{
			name:   "a description that changed on the forge, when only the title changes",
			target: target,
			params: EditParams{
				EditedFrom: Text{Title: read.Title},
				Title:      xptr.Ptr("fix(sync): read once"),
			},
			wantChange: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			change, err := tt.params.Changes(tt.target)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantChange, change)
		})
	}
}
