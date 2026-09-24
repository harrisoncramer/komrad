package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListWatchedByKeyReadsNothingWithoutKeys pins that an empty key list never falls through to the snapshot.
func TestListWatchedByKeyReadsNothingWithoutKeys(t *testing.T) {
	tests := []struct {
		name       string
		keys       []string
		wantCalled bool
		want       []string
	}{
		{
			name:       "nil keys never read the snapshot",
			keys:       nil,
			wantCalled: false,
			want:       nil,
		},
		{
			name:       "an empty key list never reads the snapshot",
			keys:       []string{},
			wantCalled: false,
			want:       nil,
		},
		{
			name:       "keys are read by key",
			keys:       []string{"a", "b"},
			wantCalled: true,
			want:       []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lister := &keyEchoingLister{}

			got, err := listWatchedByKey(t.Context(), lister, tt.keys)
			require.NoError(t, err)

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantCalled, lister.called)
		})
	}
}

// keyEchoingLister answers a read with the keys it was given.
type keyEchoingLister struct {
	called bool
}

func (l *keyEchoingLister) ListWatched(_ context.Context, keys []string) ([]string, error) {
	l.called = true

	return keys, nil
}
