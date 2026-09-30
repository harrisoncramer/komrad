package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"komrad/internal/store"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1komrad"

	screenstore "komrad/internal/store/screen"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeScreens struct {
	screen *screenstore.Screen
	err    error
}

func (f *fakeScreens) GetLastScreen(context.Context, string) (*screenstore.Screen, error) {
	if f.err != nil {
		return nil, f.err
	}

	return f.screen, nil
}

func newScreenHandler(screens *fakeScreens) agentv1komrad.AgentServiceAdapter {
	return agentv1komrad.AgentServiceAdapter{Impl: NewHandler(NewHandlerParams{Screens: screens})}
}

// TestGetLastScreen covers reading an agent's saved screen.
func TestGetLastScreen(t *testing.T) {
	captured := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		screens  *fakeScreens
		want     *agentv1.GetLastScreenResponse
		wantCode connect.Code
	}{
		{
			name: "a saved screen comes back with its size and time",
			screens: &fakeScreens{screen: &screenstore.Screen{
				AgentID:    alphaID,
				Frame:      "❯ hello",
				Width:      120,
				Height:     40,
				CapturedAt: captured,
			}},
			want: &agentv1.GetLastScreenResponse{
				Frame:      "❯ hello",
				Width:      120,
				Height:     40,
				CapturedAt: timestamppb.New(captured),
			},
		},
		{
			name:     "an agent with no saved screen is not found",
			screens:  &fakeScreens{err: fmt.Errorf("no screen: %w", store.ErrNotFound)},
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a store that fails is internal",
			screens:  &fakeScreens{err: errors.New("the database went away")},
			wantCode: connect.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newScreenHandler(tt.screens).GetLastScreen(t.Context(), connect.NewRequest(&agentv1.GetLastScreenRequest{AgentId: alphaID}))

			if tt.wantCode != 0 {
				require.Equal(t, tt.wantCode, connect.CodeOf(err))

				return
			}

			require.NoError(t, err)
			require.Empty(t, cmp.Diff(tt.want, got.Msg, protocmp.Transform()))
		})
	}
}
