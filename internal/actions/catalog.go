package actions

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"weave/lib/xslice"
)

var ErrActionNotFound = errors.New("action not found")

type Catalog struct {
	mu    sync.Mutex
	cache map[string]dirScan
}

// NewCatalog builds an empty catalog.
func NewCatalog() *Catalog {
	return &Catalog{}
}

// List returns every action across dirs in name order, where an earlier dir's file wins over
// a later one offering the same name.
func (c *Catalog) List(dirs ...string) ([]Action, error) {
	var all []Action

	for _, dir := range dirs {
		if dir == "" {
			continue
		}

		listed, err := c.scan(dir)
		if err != nil {
			return nil, err
		}

		all = append(all, listed...)
	}

	found := xslice.UniqueBy(all, func(action Action) string { return action.Name })
	slices.SortFunc(found, func(a, b Action) int { return strings.Compare(a.Name, b.Name) })

	return found, nil
}

// Find is the one action named name across dirs.
func (c *Catalog) Find(name string, dirs ...string) (Action, error) {
	found, err := c.List(dirs...)
	if err != nil {
		return Action{}, err
	}

	for _, action := range found {
		if action.Name == name {
			return action, nil
		}
	}

	var primary string
	if len(dirs) > 0 {
		primary = dirs[0]
	}

	return Action{}, fmt.Errorf("%w: %q is not in %s", ErrActionNotFound, name, primary)
}

type dirScan struct {
	modTime time.Time
	actions []Action
}

func (c *Catalog) scan(dir string) ([]Action, error) {
	modTime, err := dirModTime(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to stat the actions directory %s: %w", dir, err)
	}

	c.mu.Lock()
	cached, ok := c.cache[dir]
	c.mu.Unlock()
	if ok && cached.modTime.Equal(modTime) {
		return cached.actions, nil
	}

	listed, err := Discover(dir)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	if c.cache == nil {
		c.cache = make(map[string]dirScan)
	}
	c.cache[dir] = dirScan{modTime: modTime, actions: listed}
	c.mu.Unlock()

	return listed, nil
}

func dirModTime(dir string) (time.Time, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}

		return time.Time{}, err
	}

	return info.ModTime(), nil
}
