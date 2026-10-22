package paneltest

import (
	"context"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

const maxSettleSteps = 1000

// droppedCmds is the timers the panel starts outside its clock, which no story waits on.
var droppedCmds = []string{
	"bubbletea/v2.Tick.",
	"bubbletea/v2.Every.",
	"cursor.(*Model).Blink.",
}

type gate struct {
	mu     sync.Mutex
	held   map[string]bool
	parked chan parkedCall
}

type parkedCall struct {
	name    string
	release chan struct{}
}

type parkedCmd struct {
	call parkedCall
	done chan tea.Msg
}

// clock is the panel's time, which moves only as a story advances it.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

// armMsg is a wait the panel started on the clock, answered once the clock reaches it.
type armMsg struct {
	wait time.Duration
	fire func(time.Time) tea.Msg
}

type wait struct {
	due  time.Time
	seq  int
	fire func(time.Time) tea.Msg
}

// inbox is what the panel hands itself from off its own loop, waiting for the driver to deliver.
type inbox struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *clock) After(wait time.Duration, fire func(time.Time) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return armMsg{wait: wait, fire: fire}
	}
}

func (c *clock) set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = now
}

func (b *inbox) Send(msg tea.Msg) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.msgs = append(b.msgs, msg)
}

func (b *inbox) take() []tea.Msg {
	b.mu.Lock()
	defer b.mu.Unlock()

	msgs := b.msgs
	b.msgs = nil

	return msgs
}

// settle runs a command and every command its messages lead to, until the panel is still.
func (p *Panel) settle(cmd tea.Cmd) {
	p.t.Helper()

	queue := []tea.Cmd{cmd}
	for steps := 0; ; steps++ {
		posted := p.inbox.take()
		for i := len(posted) - 1; i >= 0; i-- {
			msg := posted[i]
			queue = slices.Insert(queue, 0, func() tea.Msg { return msg })
		}
		if len(queue) == 0 {
			return
		}
		require.Less(p.t, steps, maxSettleSteps, "the panel never settled")

		next := queue[0]
		queue = queue[1:]
		if next == nil || isDropped(next) {
			continue
		}

		msg, isDone := p.run(next)
		if !isDone {
			continue
		}

		switch msg := msg.(type) {
		case nil:
		case tea.QuitMsg:
			p.quit = true
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case armMsg:
			p.arm(msg)
		default:
			queue = append(queue, p.update(msg))
		}
	}
}

// update hands the panel one message the way the program does, through the filter first.
func (p *Panel) update(msg tea.Msg) tea.Cmd {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		p.sent = size
		p.tmux.told(size.Width, size.Height)
	}

	msg = p.panel.Filter(p.panel, msg)
	if msg == nil {
		return nil
	}

	_, cmd := p.panel.Update(msg)

	return cmd
}

// answerTerminal tells the panel the size tmux moved its pane to, as the terminal does.
func (p *Panel) answerTerminal() {
	p.t.Helper()

	for range maxSettleSteps {
		size := tea.WindowSizeMsg{
			Width:  p.tmux.GetPaneWidth(p.pane),
			Height: p.tmux.GetPaneHeight(p.pane),
		}
		if size == p.sent {
			return
		}

		p.settle(func() tea.Msg { return size })
	}

	require.Fail(p.t, "the terminal never settled on a size")
}

func (p *Panel) arm(msg armMsg) {
	p.armed++
	p.waits = append(p.waits, wait{
		due:  p.clock.Now().Add(msg.wait),
		seq:  p.armed,
		fire: msg.fire,
	})
}

// takeDue is the earliest wait due by at, in the order the waits were started.
func (p *Panel) takeDue(at time.Time) (wait, bool) {
	next := -1
	for i, armed := range p.waits {
		if armed.due.After(at) {
			continue
		}
		if next < 0 || armed.due.Before(p.waits[next].due) || armed.due.Equal(p.waits[next].due) && armed.seq < p.waits[next].seq {
			next = i
		}
	}
	if next < 0 {
		return wait{}, false
	}

	due := p.waits[next]
	p.waits = slices.Delete(p.waits, next, next+1)

	return due, true
}

func (p *Panel) run(cmd tea.Cmd) (tea.Msg, bool) {
	done := make(chan tea.Msg, 1)
	go func() {
		done <- cmd()
	}()

	return p.await(done)
}

func (p *Panel) await(done chan tea.Msg) (tea.Msg, bool) {
	select {
	case msg := <-done:
		return msg, true
	case call := <-p.gate.parked:
		p.parked = append(p.parked, parkedCmd{
			call: call,
			done: done,
		})

		return nil, false
	}
}

func (p *Panel) releaseParked() {
	p.gate.mu.Lock()
	clear(p.gate.held)
	p.gate.mu.Unlock()

	for _, parked := range p.parked {
		close(parked.call.release)
	}
	p.parked = nil
}

func (g *gate) wait(ctx context.Context, name string) {
	g.mu.Lock()
	isHeld := g.held[name]
	g.mu.Unlock()
	if !isHeld {
		return
	}

	release := make(chan struct{})
	select {
	case g.parked <- parkedCall{
		name:    name,
		release: release,
	}:
	case <-ctx.Done():
		return
	}

	select {
	case <-release:
	case <-ctx.Done():
	}
}

func isDropped(cmd tea.Cmd) bool {
	name := runtime.FuncForPC(reflect.ValueOf(cmd).Pointer()).Name()
	for _, dropped := range droppedCmds {
		if strings.Contains(name, dropped) {
			return true
		}
	}

	return false
}
