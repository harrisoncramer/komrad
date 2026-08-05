package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRef(t *testing.T) {
	tests := []struct {
		name  string
		typed string
		want  string
		wantO bool
	}{
		{
			name:  "a letter the alphabet leaves out is not a ref",
			typed: "7KQ9U",
			want:  "",
			wantO: false,
		},
		{
			name:  "punctuation the panel never prints is not a ref",
			typed: "7KQ9/B",
			want:  "",
			wantO: false,
		},
		{
			name:  "nothing typed maps to nothing",
			typed: "",
			want:  "",
			wantO: true,
		},
		{
			name:  "lowercase is raised",
			typed: "7kq9ab",
			want:  "7KQ9AB",
			wantO: true,
		},
		{
			name:  "the letters that look like digits are read as digits",
			typed: "IL0OZ",
			want:  "1100Z",
			wantO: true,
		},
		{
			name:  "dashes and spaces someone typed for legibility are dropped",
			typed: "7KQ 9-AB",
			want:  "7KQ9AB",
			wantO: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeRef(tt.typed)
			assert.Equal(t, tt.wantO, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestNewRefStaysInTheAlphabet is what makes the round trip through normalizeRef safe: a
// minted ref that carried a symbol outside the alphabet could never be typed back in.
func TestNewRefStaysInTheAlphabet(t *testing.T) {
	minted := make(map[string]bool)
	for range 200 {
		ref, err := newRef()
		require.NoError(t, err)
		require.Len(t, ref, refLength)

		for _, symbol := range ref {
			assert.True(t, strings.ContainsRune(refAlphabet, symbol), "%q carries %q", ref, symbol)
		}

		minted[ref] = true
	}

	assert.Greater(t, len(minted), 190, "the refs should not repeat")
}

func TestCreateMintsARef(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "api", State: StateIdle}))
	require.NoError(t, st.Create(ctx, Agent{Name: "web", State: StateIdle}))

	api, err := st.Get(ctx, "api")
	require.NoError(t, err)
	web, err := st.Get(ctx, "web")
	require.NoError(t, err)

	assert.Len(t, api.Ref, refLength)
	assert.NotEqual(t, api.Ref, web.Ref)

	suffix, ok := normalizeRef(api.Ref)
	require.True(t, ok)
	assert.Equal(t, api.Ref, suffix, "a minted ref is already normalized")
}

// TestCreateKeepsAGivenRef covers a caller that brought its own, which is how a test or a
// restore names an agent it already knows the ref of.
func TestCreateKeepsAGivenRef(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "api", Ref: "7KQ9AB", State: StateIdle}))

	api, err := st.Get(ctx, "api")
	require.NoError(t, err)
	assert.Equal(t, "7KQ9AB", api.Ref)
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name string
		// typed is what the caller passed, with %s standing in for the id of the agent
		// seeded first, since a test cannot know it before it is written.
		typed    string
		wantName string
		wantErr  error
	}{
		{
			name:    "a suffix under the minimum names nothing on purpose",
			typed:   "AB",
			wantErr: ErrRefTooShort,
		},
		{
			name:    "a suffix two agents share is refused rather than guessed at",
			typed:   "9AB",
			wantErr: ErrAmbiguousRef,
		},
		{
			name:  "a ref holding a symbol no ref carries matches nothing",
			typed: "UUUUU",
		},
		{
			name:  "a well-formed ref nothing holds matches nothing",
			typed: "ZZZZZZ",
		},
		{
			name:     "the id itself still resolves",
			typed:    "%s",
			wantName: "api",
		},
		{
			name:     "a whole ref resolves",
			typed:    "7KQ9AB",
			wantName: "api",
		},
		{
			name:     "a suffix only one agent ends with resolves",
			typed:    "Q9AB",
			wantName: "api",
		},
		{
			name:     "what the caller typed is normalized before it is matched",
			typed:    "q-9ab",
			wantName: "api",
		},
		{
			name:     "an archived agent still answers to its ref",
			typed:    "PPPP",
			wantName: "old",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{Name: "api", Ref: "7KQ9AB", State: StateIdle}))
			require.NoError(t, st.Create(ctx, Agent{Name: "web", Ref: "XX39AB", State: StateIdle}))
			require.NoError(t, st.Create(ctx, Agent{Name: "old", Ref: "44PPPP", State: StateIdle}))
			require.NoError(t, st.archiveByName(ctx, "old"))

			api, err := st.Get(ctx, "api")
			require.NoError(t, err)

			got, err := st.Resolve(ctx, strings.ReplaceAll(tt.typed, "%s", api.ID))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			if tt.wantName == "" {
				assert.Nil(t, got)

				return
			}

			require.NotNil(t, got)
			assert.Equal(t, tt.wantName, got.Name)
		})
	}
}

// TestResolveReportsWhatAnAmbiguousRefMatched is the whole point of refusing rather than
// picking one: the caller is told which agents it could have meant so it can type more.
func TestResolveReportsWhatAnAmbiguousRefMatched(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "api", Ref: "7KQ9AB", State: StateIdle}))
	require.NoError(t, st.Create(ctx, Agent{Name: "web", Ref: "XX39AB", State: StateIdle}))

	_, err := st.Resolve(ctx, "9AB")
	require.ErrorIs(t, err, ErrAmbiguousRef)
	assert.Contains(t, err.Error(), "7KQ9AB (api)")
	assert.Contains(t, err.Error(), "XX39AB (web)")
}
