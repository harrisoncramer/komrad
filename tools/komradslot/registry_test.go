package main

import (
	"context"
	"io"
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
				"docker info",
				"docker compose -p komrad-test-7 down --volumes --remove-orphans",
			},
		},
		{
			name: "a full registry of live checkouts refuses",
			arrange: func(t *testing.T, reg registry, _ string) {
				for n := 1; n <= slotMax; n++ {
					holdSlot(t, reg, n, checkout(t))
				}
			},
			wantErr: "all 50 slots are held",
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

			if reg.rootOf(got) != root {
				t.Errorf("slot %d holds %q, want %q", got, reg.rootOf(got), root)
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

	if !reg.goingDown(3) {
		t.Error("a retired slot is not marked as going down")
	}

	if _, ok, _ := reg.lookup(root); ok {
		t.Error("a retired slot is still found for its checkout")
	}
}
