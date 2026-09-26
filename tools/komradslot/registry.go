package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// registry is the directory of numbered slots, each claimed by one checkout.
type registry struct {
	dir         string
	lockTimeout time.Duration
	cmds        runner
	log         *log.Logger
}

func (r registry) slotDir(n int) string {
	return filepath.Join(r.dir, strconv.Itoa(n))
}

func (r registry) getRoot(n int) string {
	root, err := os.ReadFile(filepath.Join(r.slotDir(n), "root"))
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(root))
}

// isGoingDown reports whether a live process is taking slot n down.
func (r registry) isGoingDown(n int) bool {
	owner, err := os.ReadFile(filepath.Join(r.slotDir(n), "down"))
	if err != nil {
		return false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(owner)))
	if err != nil || pid <= 0 {
		return false
	}

	return syscall.Kill(pid, 0) == nil
}

func (r registry) markDown(n int) error {
	marker := filepath.Join(r.slotDir(n), "down")
	if err := os.WriteFile(marker, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return fmt.Errorf("failed to mark slot %d as going down: %w", n, err)
	}

	return nil
}

// lookup finds the slot a checkout holds.
func (r registry) lookup(root string) (int, bool, error) {
	entries, err := os.ReadDir(r.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}

	if err != nil {
		return 0, false, fmt.Errorf("failed to read the slot registry: %w", err)
	}

	for _, entry := range entries {
		n, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}

		if r.getRoot(n) == root && !r.isGoingDown(n) {
			return n, true, nil
		}
	}

	return 0, false, nil
}

// locked runs fn holding the registry's lock.
func (r registry) locked(fn func() error) error {
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return fmt.Errorf("failed to create the slot registry: %w", err)
	}

	lock, err := os.OpenFile(filepath.Join(r.dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open the registry lock: %w", err)
	}
	defer lock.Close()

	deadline := time.Now().Add(r.lockTimeout)
	for {
		err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}

		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("failed to take the registry lock: %w", err)
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for the registry lock", r.lockTimeout)
		}

		time.Sleep(10 * time.Millisecond)
	}

	return fn()
}

// pick returns the checkout's slot, claiming a free one or marking a stale one to reclaim.
func (r registry) pick(root string) (int, bool, error) {
	n, ok, err := r.lookup(root)
	if err != nil || ok {
		return n, false, err
	}

	for n := 1; n <= slotMax; n++ {
		err := os.Mkdir(r.slotDir(n), 0o755)
		if errors.Is(err, fs.ErrExist) {
			continue
		}

		if err != nil {
			return 0, false, fmt.Errorf("failed to claim slot %d: %w", n, err)
		}

		if err := os.WriteFile(filepath.Join(r.slotDir(n), "root"), []byte(root+"\n"), 0o600); err != nil {
			return 0, false, fmt.Errorf("failed to claim slot %d: %w", n, err)
		}

		return n, false, nil
	}

	for n := 1; n <= slotMax; n++ {
		if _, err := os.Stat(r.getRoot(n)); err == nil || r.isGoingDown(n) {
			continue
		}

		if err := r.markDown(n); err != nil {
			return 0, false, err
		}

		return n, true, nil
	}

	return 0, false, fmt.Errorf("all %d slots are held by checkouts that still exist", slotMax)
}

// claim returns the checkout's slot, claiming one when it holds none.
func (r registry) claim(ctx context.Context, root string) (int, error) {
	for range 3 {
		var (
			n       int
			reclaim bool
		)

		err := r.locked(func() error {
			var err error
			n, reclaim, err = r.pick(root)

			return err
		})
		if err != nil {
			return 0, err
		}

		if !reclaim {
			return n, nil
		}

		if err := r.down(ctx, n); err != nil {
			return 0, err
		}
	}

	return 0, fmt.Errorf("could not claim a slot for %s", root)
}

// retire marks the checkout's slot as going down.
func (r registry) retire(root string) (int, bool, error) {
	var (
		n  int
		ok bool
	)

	err := r.locked(func() error {
		var err error
		n, ok, err = r.lookup(root)
		if err != nil || !ok {
			return err
		}

		return r.markDown(n)
	})

	return n, ok, err
}
