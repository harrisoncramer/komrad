package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRunner struct {
	mu    sync.Mutex
	calls []string
	fail  map[string]bool
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) error {
	call := strings.Join(append([]string{name}, args...), " ")

	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, call)
	if f.fail[call] {
		return os.ErrPermission
	}

	return nil
}

func (f *fakeRunner) probe(ctx context.Context, name string, args ...string) error {
	return f.run(ctx, name, args...)
}

func newTestRegistry(t *testing.T) (registry, *fakeRunner) {
	t.Helper()

	cmds := &fakeRunner{}

	return registry{
		dir:         filepath.Join(t.TempDir(), "instances"),
		lockTimeout: time.Second,
		cmds:        cmds,
		log:         log.New(io.Discard, "", 0),
	}, cmds
}

func checkout(t *testing.T) string {
	t.Helper()

	return t.TempDir()
}

func holdSlot(t *testing.T, reg registry, n int, root string) {
	t.Helper()

	if err := os.MkdirAll(reg.slotDir(n), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(reg.slotDir(n), "root"), []byte(root+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func markDownBy(t *testing.T, reg registry, n, pid int) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(reg.slotDir(n), "down"), []byte(strconv.Itoa(pid)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// deadPID is the pid of a process that has already exited.
func deadPID(t *testing.T) int {
	t.Helper()

	proc, err := os.StartProcess("/usr/bin/true", []string{"true"}, &os.ProcAttr{})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := proc.Wait(); err != nil {
		t.Fatal(err)
	}

	return proc.Pid
}

// TestClaim covers which slot a checkout is handed.
func TestClaim(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		arrange func(t *testing.T, reg registry, root string)
		want    int
		wantErr string
		calls   []string
	}{
		{
			name: "a full registry of live checkouts refuses",
			arrange: func(t *testing.T, reg registry, _ string) {
				for n := 1; n <= slotMax; n++ {
					holdSlot(t, reg, n, checkout(t))
				}
			},
			wantErr: "all 50 slots are held",
		},
		{
			name: "an empty registry hands out the first slot",
			want: 1,
		},
		{
			name: "a checkout keeps the slot it already holds",
			arrange: func(t *testing.T, reg registry, root string) {
				holdSlot(t, reg, 1, checkout(t))
				holdSlot(t, reg, 2, root)
			},
			want: 2,
		},
		{
			name: "a slot being taken down by a live process is not the checkout's any more",
			arrange: func(t *testing.T, reg registry, root string) {
				holdSlot(t, reg, 1, root)
				markDownBy(t, reg, 1, os.Getpid())
			},
			want: 2,
		},
		{
			name: "a down marker left by a dead process is ignored",
			arrange: func(t *testing.T, reg registry, root string) {
				holdSlot(t, reg, 1, root)
				markDownBy(t, reg, 1, deadPID(t))
			},
			want: 1,
		},
		{
			name: "a full registry reclaims a slot whose checkout is gone",
			arrange: func(t *testing.T, reg registry, _ string) {
				for n := 1; n <= slotMax; n++ {
					holdSlot(t, reg, n, checkout(t))
				}
				holdSlot(t, reg, 7, filepath.Join(t.TempDir(), "gone"))
			},
			want: 7,
			calls: []string{
				"tmux -L komrad-test-7 kill-server",
				"pkill -f komrad serve --addr 127.0.0.1:8807( |$)",
				"docker compose -p komrad-test-pg exec -T db psql -U komrad -d postgres -v ON_ERROR_STOP=1 -c DROP DATABASE IF EXISTS komrad_7 WITH (FORCE) -c DROP ROLE IF EXISTS komrad_engine_slot_7, komrad_engine_slot_7_reader",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reg, cmds := newTestRegistry(t)
			root := checkout(t)
			if tt.arrange != nil {
				tt.arrange(t, reg, root)
			}

			got, err := reg.claim(t.Context(), root)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("claim() error = %v, want one containing %q", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("claim() error = %v", err)
			}

			if got != tt.want {
				t.Errorf("claim() = %d, want %d", got, tt.want)
			}

			if reg.getRoot(got) != root {
				t.Errorf("slot %d holds %q, want %q", got, reg.getRoot(got), root)
			}

			if strings.Join(cmds.calls, "\n") != strings.Join(tt.calls, "\n") {
				t.Errorf("ran %q, want %q", cmds.calls, tt.calls)
			}
		})
	}
}

// TestClaimConcurrently hands each checkout one slot however many claims race.
func TestClaimConcurrently(t *testing.T) {
	t.Parallel()

	reg, _ := newTestRegistry(t)
	reg.lockTimeout = 30 * time.Second
	roots := make([]string, 16)
	for i := range roots {
		roots[i] = checkout(t)
	}

	slots := make([]int, len(roots)*2)
	errs := make([]error, len(slots))

	var wg sync.WaitGroup
	for i := range slots {
		wg.Go(func() {
			slots[i], errs[i] = reg.claim(t.Context(), roots[i%len(roots)])
		})
	}
	wg.Wait()

	seen := map[int]string{}
	for i, n := range slots {
		if errs[i] != nil {
			t.Fatalf("claim %d: %v", i, errs[i])
		}

		root := roots[i%len(roots)]
		if other, ok := seen[n]; ok && other != root {
			t.Fatalf("slot %d handed to both %s and %s", n, other, root)
		}

		seen[n] = root
	}

	if len(seen) != len(roots) {
		t.Errorf("%d checkouts got %d slots", len(roots), len(seen))
	}
}

// TestLockedTimesOut gives up on a lock another holder keeps.
func TestLockedTimesOut(t *testing.T) {
	t.Parallel()

	reg, _ := newTestRegistry(t)
	reg.lockTimeout = 100 * time.Millisecond

	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error)

	go func() {
		done <- reg.locked(func() error {
			close(held)
			<-release

			return nil
		})
	}()

	<-held

	err := reg.locked(func() error { return nil })
	close(release)

	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("locked() error = %v, want a timeout", err)
	}

	if err := <-done; err != nil {
		t.Errorf("holder: %v", err)
	}
}

// TestRetire takes a slot out of its checkout's hands.
func TestRetire(t *testing.T) {
	t.Parallel()

	reg, _ := newTestRegistry(t)
	root := checkout(t)
	holdSlot(t, reg, 3, root)

	n, ok, err := reg.retire(root)
	if err != nil || !ok || n != 3 {
		t.Fatalf("retire() = %d, %v, %v, want 3, true, nil", n, ok, err)
	}

	if !reg.isGoingDown(3) {
		t.Error("a retired slot is not marked as going down")
	}

	if _, ok, _ := reg.lookup(root); ok {
		t.Error("a retired slot is still found for its checkout")
	}
}

func ageSlot(t *testing.T, reg registry, n int, age time.Duration) {
	t.Helper()

	then := time.Now().Add(-age)
	if err := os.Chtimes(reg.slotDir(n), then, then); err != nil {
		t.Fatal(err)
	}
}

func downCalls(n int) []string {
	return []string{
		fmt.Sprintf("tmux -L komrad-test-%d kill-server", n),
		fmt.Sprintf("pkill -f komrad serve --addr 127.0.0.1:88%02d( |$)", n),
		fmt.Sprintf("docker compose -p komrad-test-pg exec -T db psql -U komrad -d postgres -v ON_ERROR_STOP=1 -c DROP DATABASE IF EXISTS komrad_%d WITH (FORCE) -c DROP ROLE IF EXISTS komrad_engine_slot_%d, komrad_engine_slot_%d_reader", n, n, n),
	}
}

// TestReap covers which slots are taken down and which are left alone.
func TestReap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		arrange   func(t *testing.T, reg registry, cmds *fakeRunner, keep string)
		listening bool
		reaped    bool
		calls     []string
	}{
		{
			name: "a slot whose checkout is gone is reaped",
			arrange: func(t *testing.T, reg registry, _ *fakeRunner, _ string) {
				holdSlot(t, reg, 3, filepath.Join(t.TempDir(), "gone"))
			},
			reaped: true,
			calls:  downCalls(3),
		},
		{
			name: "a slot used within a day is kept",
			arrange: func(t *testing.T, reg registry, _ *fakeRunner, _ string) {
				holdSlot(t, reg, 3, checkout(t))
				ageSlot(t, reg, 3, time.Hour)
			},
		},
		{
			name: "an idle slot with nothing running is reaped",
			arrange: func(t *testing.T, reg registry, cmds *fakeRunner, _ string) {
				holdSlot(t, reg, 3, checkout(t))
				ageSlot(t, reg, 3, 2*idleAfter)
				cmds.fail = map[string]bool{"tmux -L komrad-test-3 list-sessions": true}
			},
			reaped: true,
			calls:  append([]string{"tmux -L komrad-test-3 list-sessions"}, downCalls(3)...),
		},
		{
			name: "an idle slot whose server is still listening is kept",
			arrange: func(t *testing.T, reg registry, _ *fakeRunner, _ string) {
				holdSlot(t, reg, 3, checkout(t))
				ageSlot(t, reg, 3, 2*idleAfter)
			},
			listening: true,
		},
		{
			name: "an idle slot whose tmux server still has sessions is kept",
			arrange: func(t *testing.T, reg registry, _ *fakeRunner, _ string) {
				holdSlot(t, reg, 3, checkout(t))
				ageSlot(t, reg, 3, 2*idleAfter)
			},
			calls: []string{"tmux -L komrad-test-3 list-sessions"},
		},
		{
			name: "the claiming checkout's own slot is kept however idle",
			arrange: func(t *testing.T, reg registry, cmds *fakeRunner, keep string) {
				holdSlot(t, reg, 3, keep)
				ageSlot(t, reg, 3, 2*idleAfter)
				cmds.fail = map[string]bool{"tmux -L komrad-test-3 list-sessions": true}
			},
		},
		{
			name: "a slot a live process is taking down is left to it",
			arrange: func(t *testing.T, reg registry, _ *fakeRunner, _ string) {
				holdSlot(t, reg, 3, filepath.Join(t.TempDir(), "gone"))
				markDownBy(t, reg, 3, os.Getpid())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reg, cmds := newTestRegistry(t)
			reg.listening = func(context.Context, string) bool { return tt.listening }
			keep := checkout(t)
			tt.arrange(t, reg, cmds, keep)

			if err := reg.reap(t.Context(), keep); err != nil {
				t.Fatalf("reap() error = %v", err)
			}

			_, err := os.Stat(reg.slotDir(3))
			if gone := errors.Is(err, fs.ErrNotExist); gone != tt.reaped {
				t.Errorf("slot 3 gone = %v, want %v", gone, tt.reaped)
			}

			if strings.Join(cmds.calls, "\n") != strings.Join(tt.calls, "\n") {
				t.Errorf("ran %q, want %q", cmds.calls, tt.calls)
			}
		})
	}
}

// TestReapKeepsGoingPastASlotItCannotDrop keeps a slot whose database would not drop and reaps the rest.
func TestReapKeepsGoingPastASlotItCannotDrop(t *testing.T) {
	t.Parallel()

	reg, cmds := newTestRegistry(t)
	holdSlot(t, reg, 3, filepath.Join(t.TempDir(), "gone"))
	holdSlot(t, reg, 4, filepath.Join(t.TempDir(), "gone"))
	cmds.fail = map[string]bool{downCalls(3)[2]: true}

	if err := reg.reap(t.Context(), ""); err != nil {
		t.Fatalf("reap() error = %v", err)
	}

	if reg.getRoot(3) == "" {
		t.Error("slot 3 was given back although its database was not dropped")
	}

	if _, err := os.Stat(reg.slotDir(4)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("slot 4 was not reaped: %v", err)
	}
}

// TestClaimMarksTheSlotUsed keeps a slot that is claimed from being reaped as idle.
func TestClaimMarksTheSlotUsed(t *testing.T) {
	t.Parallel()

	reg, _ := newTestRegistry(t)
	root := checkout(t)
	holdSlot(t, reg, 1, root)
	ageSlot(t, reg, 1, 2*idleAfter)

	if _, err := reg.claim(t.Context(), root); err != nil {
		t.Fatalf("claim() error = %v", err)
	}

	info, err := os.Stat(reg.slotDir(1))
	if err != nil {
		t.Fatal(err)
	}

	if time.Since(info.ModTime()) > time.Minute {
		t.Errorf("slot 1 was last used %s ago, want just now", time.Since(info.ModTime()))
	}
}
