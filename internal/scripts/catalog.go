package scripts

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

var ErrScriptNotFound = errors.New("script not found")

type Catalog struct {
	mu    sync.Mutex
	cache map[string]dirScan
}

// NewCatalog builds an empty catalog.
func NewCatalog() *Catalog {
	return &Catalog{}
}

// List returns every script across dirs in name order, where an earlier dir's file wins over
// a later one offering the same name.
func (c *Catalog) List(dirs ...string) ([]Script, error) {
	var all []Script

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

	found := xslice.UniqueBy(all, func(script Script) string { return script.Name })
	slices.SortFunc(found, func(a, b Script) int { return strings.Compare(a.Name, b.Name) })

	return found, nil
}

// Find is the one script named name across dirs.
func (c *Catalog) Find(name string, dirs ...string) (Script, error) {
	found, err := c.List(dirs...)
	if err != nil {
		return Script{}, err
	}

	for _, script := range found {
		if script.Name == name {
			return script, nil
		}
	}

	var primary string
	if len(dirs) > 0 {
		primary = dirs[0]
	}

	return Script{}, fmt.Errorf("%w: %q is not in %s", ErrScriptNotFound, name, primary)
}

type dirScan struct {
	modTime time.Time
	scripts []Script
}

func (c *Catalog) scan(dir string) ([]Script, error) {
	modTime, err := dirModTime(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to stat the scripts directory %s: %w", dir, err)
	}

	c.mu.Lock()
	cached, ok := c.cache[dir]
	c.mu.Unlock()
	if ok && cached.modTime.Equal(modTime) {
		return cached.scripts, nil
	}

	listed, err := Discover(dir)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	if c.cache == nil {
		c.cache = make(map[string]dirScan)
	}
	c.cache[dir] = dirScan{modTime: modTime, scripts: listed}
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
