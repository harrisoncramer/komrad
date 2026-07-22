package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "agents.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

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
