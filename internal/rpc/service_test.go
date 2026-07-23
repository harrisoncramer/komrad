package rpc

import (
	"context"
	"path/filepath"
	"testing"

	"claude-agents/internal/store"
	sessionv1 "claude-agents/proto/gen/session/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

func service(t *testing.T) (*SessionService, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "agents.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewSessionService(st), st
}

func TestListAndGetSession(t *testing.T) {
	svc, st := service(t)
	ctx := context.Background()
	_ = st.Upsert(ctx, store.Agent{Name: "alpha", Cwd: "/tmp", State: store.StateThinking})

	list, err := svc.ListSessions(ctx, connect.NewRequest(&emptypb.Empty{}))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := list.Msg.GetSessions(); len(got) != 1 || got[0].GetName() != "alpha" {
		t.Fatalf("unexpected list: %+v", got)
	}

	got, err := svc.GetSession(ctx, connect.NewRequest(&sessionv1.GetSessionRequest{Name: "alpha"}))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Msg.GetSession().GetCwd() != "/tmp" || got.Msg.GetSession().GetState() != store.StateThinking {
		t.Fatalf("get round-trip mismatch: %+v", got.Msg.GetSession())
	}
}

func TestGetMissingSessionIsNotFound(t *testing.T) {
	svc, _ := service(t)

	_, err := svc.GetSession(context.Background(), connect.NewRequest(&sessionv1.GetSessionRequest{Name: "ghost"}))
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("want NotFound, got %v", err)
	}
}

func TestSetStateThroughService(t *testing.T) {
	svc, st := service(t)
	ctx := context.Background()
	_ = st.Upsert(ctx, store.Agent{Name: "alpha", State: store.StateThinking})

	if _, err := svc.SetState(ctx, connect.NewRequest(&sessionv1.SetStateRequest{Name: "alpha", State: store.StateDone})); err != nil {
		t.Fatalf("set state: %v", err)
	}
	got, _ := st.Get(ctx, "alpha")
	if got.State != store.StateDone {
		t.Fatalf("want done, got %q", got.State)
	}
}

func TestUpdateSessionRenamesByID(t *testing.T) {
	svc, st := service(t)
	ctx := context.Background()
	_ = st.Upsert(ctx, store.Agent{Name: "alpha", State: store.StateNew})
	original, _ := st.Get(ctx, "alpha")

	if _, err := svc.UpdateSession(ctx, connect.NewRequest(&sessionv1.UpdateSessionRequest{Id: original.ID, Name: "renamed"})); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got, _ := st.Get(ctx, "renamed"); got == nil || got.ID != original.ID {
		t.Fatalf("rename did not preserve id: %+v", got)
	}
	if got, _ := st.Get(ctx, "alpha"); got != nil {
		t.Fatalf("old name still resolves: %+v", got)
	}
}

func TestDeleteSessionThroughService(t *testing.T) {
	svc, st := service(t)
	ctx := context.Background()
	_ = st.Upsert(ctx, store.Agent{Name: "alpha", State: store.StateNew})

	if _, err := svc.DeleteSession(ctx, connect.NewRequest(&sessionv1.DeleteSessionRequest{Name: "alpha"})); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got, _ := st.Get(ctx, "alpha"); got != nil {
		t.Fatalf("want gone after hard delete, got %+v", got)
	}
}
