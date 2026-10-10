package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"sync"
)

// StackLoader reads a config stack and reads it again only once one of its files has changed.
type StackLoader struct {
	stack  Stack
	mu     sync.Mutex
	loaded bool
	stamps []os.FileInfo
	cfg    *Config
	err    error
}

// NewStackLoader builds a loader for stack that reads nothing until it is first asked.
func NewStackLoader(stack Stack) *StackLoader {
	return &StackLoader{stack: stack}
}

// Load reads the stack's config, reusing the last read while no file in the stack has changed.
func (l *StackLoader) Load() (*Config, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	stamps, err := l.stampLayers()
	if err != nil {
		return nil, err
	}

	if l.loaded && slices.EqualFunc(l.stamps, stamps, isSameStamp) {
		return l.cfg, l.err
	}

	l.cfg, l.err = LoadStack(l.stack)
	l.stamps = stamps
	l.loaded = true

	return l.cfg, l.err
}

func (l *StackLoader) stampLayers() ([]os.FileInfo, error) {
	layers, err := ResolveLayers(l.stack)
	if err != nil {
		return nil, err
	}

	stamps := make([]os.FileInfo, len(layers))
	for i, layer := range layers {
		info, err := os.Stat(layer.Path)
		if err == nil {
			stamps[i] = info
			continue
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("failed to read config %s: %w", layer.Path, err)
		}
		if link, err := os.Lstat(layer.Path); err == nil {
			stamps[i] = link
		}
	}

	return stamps, nil
}

// isSameStamp reports whether two stats are of one unchanged file, inode included.
func isSameStamp(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}
