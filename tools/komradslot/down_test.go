package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDown covers when a slot is given back and when its claim is kept.
func TestDown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fail      string
		wantErr   bool
		wantKept  bool
		wantCalls int
	}{
		{
			name:      "docker being unreachable keeps the claim",
			fail:      "docker info",
			wantErr:   true,
			wantKept:  true,
			wantCalls: 3,
		},
		{
			name:      "compose failing keeps the claim",
			fail:      "docker compose -p komrad-test-2 down --volumes --remove-orphans",
			wantErr:   true,
			wantKept:  true,
			wantCalls: 4,
		},
		{
			name:      "tmux and the server already being gone is not a failure",
			fail:      "tmux -L komrad-test-2 kill-server",
			wantCalls: 4,
		},
		{
			name:      "a slot whose database comes down is given back",
			wantCalls: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reg, cmds := newTestRegistry(t)
			cmds.fail = map[string]bool{tt.fail: true}
			holdSlot(t, reg, 2, checkout(t))
			markDownBy(t, reg, 2, os.Getpid())

			err := reg.down(t.Context(), 2)

			if (err != nil) != tt.wantErr {
				t.Fatalf("down() error = %v, wantErr %v", err, tt.wantErr)
			}

			_, statErr := os.Stat(reg.slotDir(2))
			if kept := statErr == nil; kept != tt.wantKept {
				t.Errorf("slot kept = %v, want %v", kept, tt.wantKept)
			}

			if tt.wantKept {
				if _, err := os.Stat(filepath.Join(reg.slotDir(2), "down")); err == nil {
					t.Error("a kept claim still carries its down marker")
				}
			}

			if len(cmds.calls) != tt.wantCalls {
				t.Errorf("ran %q, want %d commands", cmds.calls, tt.wantCalls)
			}
		})
	}
}

// TestDownRefusesANumberOutsideTheRegistry runs nothing for a slot that cannot exist.
func TestDownRefusesANumberOutsideTheRegistry(t *testing.T) {
	t.Parallel()

	reg, cmds := newTestRegistry(t)
	for _, n := range []int{0, -1, slotMax + 1} {
		if err := reg.down(t.Context(), n); err == nil {
			t.Errorf("down(%d) succeeded", n)
		}

		if len(cmds.calls) != 0 {
			t.Errorf("down(%d) ran %q", n, cmds.calls)
		}
	}
}
