package store

import (
	"context"
	"path/filepath"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "agents.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// TestReuseNameAfterSoftDelete is the core of the identity fix: a soft-deleted
// row keeps its name, and reusing that name must create a fresh row rather than
// resurrect the dead one. State events keyed by name must land on the live row.
func TestReuseNameAfterSoftDelete(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	if err := st.Upsert(ctx, Agent{Name: "agent", Pane: "%1", State: StateThinking}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	first, err := st.Get(ctx, "agent")
	if err != nil || first == nil {
		t.Fatalf("get first: %v", err)
	}

	if err := st.SoftDelete(ctx, "agent"); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if got, _ := st.Get(ctx, "agent"); got != nil {
		t.Fatalf("get after soft-delete should skip deleted row, got %+v", got)
	}

	if err := st.Upsert(ctx, Agent{Name: "agent", Pane: "%2", State: StateNew}); err != nil {
		t.Fatalf("reuse upsert: %v", err)
	}
	second, err := st.Get(ctx, "agent")
	if err != nil || second == nil {
		t.Fatalf("get second: %v", err)
	}

	if second.ID == first.ID {
		t.Fatalf("reused name resurrected the deleted row: id %q", second.ID)
	}
	if second.State != StateNew || second.Pane != "%2" {
		t.Fatalf("live row not fresh: %+v", second)
	}

	live, _ := st.List(ctx)
	if len(live) != 1 || live[0].ID != second.ID {
		t.Fatalf("want one live row (the fresh one), got %+v", live)
	}
	all, _ := st.ListAll(ctx)
	if len(all) != 2 {
		t.Fatalf("want both rows retained, got %d", len(all))
	}

	// A state event keyed by name must update the live row, leaving the deleted
	// row's state frozen at what it was when deleted.
	if err := st.UpsertState(ctx, "agent", "%2", StateWaiting, ""); err != nil {
		t.Fatalf("upsert state: %v", err)
	}
	if got, _ := st.Get(ctx, "agent"); got == nil || got.ID != second.ID || got.State != StateWaiting {
		t.Fatalf("state event landed on wrong row: %+v", got)
	}
	for _, a := range mustListAll(t, st) {
		if a.ID == first.ID && a.State != StateThinking {
			t.Fatalf("deleted row was mutated: %+v", a)
		}
	}
}

func TestActiveNameExists(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	if exists, _ := st.ActiveNameExists(ctx, "agent"); exists {
		t.Fatal("no agent yet, want false")
	}

	_ = st.Upsert(ctx, Agent{Name: "agent", State: StateNew})
	if exists, _ := st.ActiveNameExists(ctx, "agent"); !exists {
		t.Fatal("live agent, want true")
	}

	_ = st.SoftDelete(ctx, "agent")
	if exists, _ := st.ActiveNameExists(ctx, "agent"); exists {
		t.Fatal("only a deleted agent holds the name, want false")
	}
}

// TestDeleteThenRestore mirrors the manager's restore ordering at the store
// layer: undelete by id, then upsert the same id. It must not collide on the
// primary key, and it must leave exactly one live row keyed to the same id and
// conversation.
func TestDeleteThenRestore(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	_ = st.Upsert(ctx, Agent{Name: "beta", State: StateThinking, ClaudeSessionID: "sess-1"})
	original, _ := st.Get(ctx, "beta")
	_ = st.SoftDelete(ctx, "beta")

	if err := st.Undelete(ctx, original.ID); err != nil {
		t.Fatalf("undelete: %v", err)
	}
	restored := *original
	restored.State = StateNew
	restored.Pane = "%9"
	if err := st.Upsert(ctx, restored); err != nil {
		t.Fatalf("upsert on restore collided: %v", err)
	}

	got, _ := st.Get(ctx, "beta")
	if got == nil || got.ID != original.ID {
		t.Fatalf("restored row lost its id: %+v", got)
	}
	if got.State != StateNew || got.ClaudeSessionID != "sess-1" {
		t.Fatalf("restore did not preserve conversation or reset state: %+v", got)
	}
	if all, _ := st.ListAll(ctx); len(all) != 1 {
		t.Fatalf("want a single row after restore, got %d", len(all))
	}
}

func mustListAll(t *testing.T, st *Store) []Agent {
	t.Helper()
	all, err := st.ListAll(context.Background())
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	return all
}

func TestStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "agents.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = st.Close() }()

	if err := st.Upsert(ctx, Agent{Name: "alpha", Pane: "%1", Cwd: "/tmp", Flags: "--model opus", Prompt: "do the thing", State: "new"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	agents, err := st.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("want 1 agent, got %d", len(agents))
	}
	if agents[0].ID == "" {
		t.Fatalf("want generated id, got empty")
	}
	if agents[0].Cwd != "/tmp" || agents[0].Flags != "--model opus" {
		t.Fatalf("round-trip mismatch: %+v", agents[0])
	}

	if err := st.SetState(ctx, "alpha", "done"); err != nil {
		t.Fatalf("set state: %v", err)
	}

	if err := st.Delete(ctx, "alpha"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	agents, err = st.List(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(agents) != 0 {
		t.Fatalf("want 0 agents after delete, got %d", len(agents))
	}
}
