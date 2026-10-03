package ui

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

const sizeSettle = 500 * time.Millisecond

type sizeRecheckMsg struct{}

type sizeFilter struct {
	now        func() time.Time
	later      func(tea.Msg)
	mu         sync.Mutex
	expected   *tea.WindowSizeMsg
	at         time.Time
	rechecking bool
}

func newSizeFilter(now func() time.Time, later func(tea.Msg)) *sizeFilter {
	return &sizeFilter{
		now:   now,
		later: later,
	}
}

func (f *sizeFilter) expect(width, height int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.expected = &tea.WindowSizeMsg{Width: width, Height: height}
	f.at = f.now()
}

func (f *sizeFilter) filter(_ tea.Model, msg tea.Msg) tea.Msg {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := msg.(sizeRecheckMsg); ok {
		f.expected = nil
		f.rechecking = false

		return msg
	}

	size, ok := msg.(tea.WindowSizeMsg)
	if !ok || f.expected == nil || size == *f.expected || f.now().Sub(f.at) >= sizeSettle {
		return msg
	}

	if !f.rechecking {
		f.rechecking = true
		f.later(sizeRecheckMsg{})
	}

	return nil
}
