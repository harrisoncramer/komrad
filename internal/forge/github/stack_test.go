package github

import (
	"net/http"
	"testing"

	"komrad/internal/forge"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStack covers creating a GitHub stack, adding to one, and GitHub's refusals.
func TestStack(t *testing.T) {
	t.Parallel()

	const (
		stacks = "/repos/harrisoncramer/komrad-forge-fixture/stacks"
		add    = stacks + "/5/add"
	)
	stacked := answer{status: http.StatusCreated, body: `{"number":5,"open":true}`}

	tests := []struct {
		name string
		// enterprise says the host lacks GitHub stacks.
		enterprise bool
		params     forge.StackParams
		answers    map[string]answer
		wantErr    error
		wantPath   string
		wantBody   map[string]any
	}{
		{
			name:       "a host without github stacks is refused before github is asked",
			enterprise: true,
			params:     forge.StackParams{Numbers: []int{3, 4}},
			wantErr:    forge.ErrNotStackable,
		},
		{
			name:    "github refuses pull requests that do not chain",
			params:  forge.StackParams{Numbers: []int{3, 4}},
			answers: map[string]answer{stacks: {status: http.StatusUnprocessableEntity, body: `{"message":"Pull requests must form a stack"}`}},
			wantErr: forge.ErrNotStackable,
		},
		{
			name:    "github has no stacks for the repository",
			params:  forge.StackParams{Numbers: []int{3, 4}},
			wantErr: forge.ErrNotStackable,
		},
		{
			name:    "a credential github will not let stack",
			params:  forge.StackParams{Numbers: []int{3, 4}},
			answers: map[string]answer{stacks: {status: http.StatusUnauthorized, body: `{"message":"Bad credentials"}`}},
			wantErr: forge.ErrUnauthorized,
		},
		{
			name:     "the pull requests go in a new stack",
			params:   forge.StackParams{Numbers: []int{3, 4}},
			answers:  map[string]answer{stacks: stacked},
			wantPath: stacks,
			wantBody: map[string]any{"pull_requests": []any{float64(3), float64(4)}},
		},
		{
			name:     "the pull requests go on top of the stack numbered",
			params:   forge.StackParams{Stack: 5, Numbers: []int{6}},
			answers:  map[string]answer{add: stacked},
			wantPath: add,
			wantBody: map[string]any{"pull_requests": []any{float64(6)}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, tt.answers)
			f.stacks = !tt.enterprise

			params := tt.params
			params.Repo = fixture
			err := f.Stack(t.Context(), params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantBody, fake.sentTo(t, tt.wantPath).Body)
		})
	}
}

// TestRetarget covers taking a pull request out of its GitHub stack and changing its base branch.
func TestRetarget(t *testing.T) {
	t.Parallel()

	const (
		address = "https://github.com/harrisoncramer/komrad-forge-fixture/pull/4"
		stacks  = "/repos/harrisoncramer/komrad-forge-fixture/stacks"
		unstack = stacks + "/5/unstack"
		pull    = "/repos/harrisoncramer/komrad-forge-fixture/pulls/4"
	)
	inStack := answer{body: `[
	{
		"number": 5,
		"open": true,
		"pull_requests": [
			{
				"number": 3,
				"state": "closed",
				"merged_at": null
			},
			{
				"number": 4,
				"state": "open",
				"merged_at": null
			}
		]
	}
]`}
	retargeted := answer{body: `{"number":4}`}

	tests := []struct {
		name      string
		answers   map[string]answer
		wantErr   error
		wantPaths []string
	}{
		{
			name: "github refuses to take the pull request out of its stack",
			answers: map[string]answer{
				stacks:  inStack,
				unstack: {status: http.StatusUnprocessableEntity, body: `{"message":"Stack cannot be modified"}`},
			},
			wantErr: forge.ErrNotStackable,
		},
		{
			name:    "a pull request github does not have",
			answers: map[string]answer{stacks: {body: `[]`}},
			wantErr: forge.ErrNotFound,
		},
		{
			name: "github refuses the base branch",
			answers: map[string]answer{
				stacks: {body: `[]`},
				pull:   {status: http.StatusUnprocessableEntity, body: `{"message":"Validation Failed"}`},
			},
			wantErr: forge.ErrNotStackable,
		},
		{
			name: "a pull request in no stack changes its base branch",
			answers: map[string]answer{
				stacks: {body: `[]`},
				pull:   retargeted,
			},
			wantPaths: []string{stacks, pull},
		},
		{
			name: "a pull request in a stack leaves it and changes its base branch",
			answers: map[string]answer{
				stacks:  inStack,
				unstack: {status: http.StatusNoContent},
				pull:    retargeted,
			},
			wantPaths: []string{stacks, unstack, pull},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, tt.answers)

			err := f.Retarget(t.Context(), address, "main")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			paths := xslice.Map(fake.calls, func(seen call) string { return seen.Path })
			assert.Equal(t, tt.wantPaths, paths)
			assert.Equal(t, map[string]any{"base": "main"}, fake.sentTo(t, pull).Body)
		})
	}
}
