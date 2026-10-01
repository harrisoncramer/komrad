// Package vim is vim's normal and insert modes over a text buffer, one key at a time.
package vim

import (
	"slices"
	"strings"
)

// Mode is what the keys typed into a field do.
type Mode int

const (
	// Insert passes typed text to the field.
	Insert Mode = iota
	// Normal reads keys as commands.
	Normal
)

// Outcome is what the caller does with a key the engine was handed.
type Outcome int

const (
	// Consumed means the engine took the key and the result's buffer is what the field holds now.
	Consumed Outcome = iota
	// Pass means the field handles the key itself.
	Pass
	// EdgeUp means the cursor tried to leave the buffer upward.
	EdgeUp
	// EdgeDown means the cursor tried to leave the buffer downward.
	EdgeDown
	// Leave means esc was pressed in normal mode with nothing pending.
	Leave
)

// Buffer is a field's text and its cursor as an offset in runes.
type Buffer struct {
	Text   string
	Cursor int
}

// Result is what one key did.
type Result struct {
	Outcome Outcome
	Buffer  Buffer
}

// Register holds the text last yanked or deleted, shared by every engine handed it.
type Register struct {
	text     string
	linewise bool
}

// historyLimit is how many undo steps an engine keeps.
const historyLimit = 200

// change is the last command that changed the text.
type change struct {
	cmd      command
	inserts  bool
	inserted string
}

// Engine is vim's state for one field.
type Engine struct {
	singleLine bool
	register   *Register
	passes     []string
	mode       Mode
	pending    []string

	undo []Buffer
	redo []Buffer

	// last is the buffer the engine last left.
	last Buffer

	// before is the buffer ahead of the command that began the current insert.
	before Buffer
	// typedFrom is the buffer as the current insert began.
	typedFrom Buffer
	recording *change
	repeat    *change
	lastFind  *find
}

// New is an engine for one field, starting in insert mode on b, that hands passes on to the field in normal mode.
func New(b Buffer, singleLine bool, register *Register, passes []string) *Engine {
	if register == nil {
		register = &Register{}
	}

	b = clamp(b)

	return &Engine{
		singleLine: singleLine,
		register:   register,
		passes:     passes,
		mode:       Insert,
		before:     b,
		typedFrom:  b,
		last:       b,
	}
}

// Mode is the mode the engine is in.
func (e *Engine) Mode() Mode {
	return e.mode
}

// Insert puts the engine in insert mode on b, as a field does when the cursor lands on it.
func (e *Engine) Insert(b Buffer) {
	if e.mode == Insert {
		return
	}

	b = clamp(b)
	e.mode = Insert
	e.pending = nil
	e.before, e.typedFrom = b, b
	e.recording = nil
}

// Normal puts the engine in normal mode on b and is the buffer the field then holds.
func (e *Engine) Normal(b Buffer) Buffer {
	if e.mode == Insert {
		return e.leaveInsert(b).Buffer
	}

	b = e.settle(b)
	e.last = b

	return b
}

// Key hands the engine one key, named the way bubbletea names it, with the field's buffer as it stands.
func (e *Engine) Key(key string, b Buffer) Result {
	b = clamp(b)

	if e.mode == Insert {
		if isEsc(key) {
			return e.leaveInsert(b)
		}

		return Result{Outcome: Pass, Buffer: b}
	}

	if b.Text != e.last.Text {
		e.record(e.last)
	}

	if isEsc(key) {
		if len(e.pending) > 0 {
			e.pending = nil

			return e.done(Consumed, b)
		}

		return e.done(Leave, b)
	}

	if !e.owns(key) {
		e.pending = nil
		if slices.Contains(e.passes, key) {
			return e.done(Pass, b)
		}

		return e.done(Consumed, b)
	}

	e.pending = append(e.pending, key)

	c, status := parse(e.pending)
	switch status {
	case parseIncomplete:
		return e.done(Consumed, b)
	case parseInvalid:
		e.pending = nil

		return e.done(Consumed, b)
	case parseComplete:
	}

	e.pending = nil

	return e.run(c, b)
}

// owns reports whether a key in normal mode is the engine's rather than the field's.
func (e *Engine) owns(key string) bool {
	if awaitsChar(e.pending) {
		_, ok := keyChar(key)

		return ok
	}

	switch key {
	case "space", "left", "right", "up", "down", "backspace", "delete", "ctrl+r":
		return true
	case "enter":
		return !e.singleLine
	}

	_, ok := keyChar(key)

	return ok
}

func isEsc(key string) bool {
	return key == "esc" || key == "ctrl+["
}

func (e *Engine) done(outcome Outcome, b Buffer) Result {
	e.last = b

	return Result{Outcome: outcome, Buffer: b}
}

func (e *Engine) record(b Buffer) {
	e.undo = append(e.undo, b)
	if len(e.undo) > historyLimit {
		e.undo = e.undo[1:]
	}
	e.redo = nil
}

func (e *Engine) run(c command, b Buffer) Result {
	switch c.name {
	case "u":
		for range c.times() {
			b = step(&e.undo, &e.redo, b)
		}

		return e.done(Consumed, e.settle(b))
	case "ctrl+r":
		for range c.times() {
			b = step(&e.redo, &e.undo, b)
		}

		return e.done(Consumed, e.settle(b))
	case ".":
		return e.replay(c, b)
	}

	if c.op == "" && (c.name == "j" || c.name == "k") {
		if edge, ok := e.edge(c, b); ok {
			return e.done(edge, b)
		}
	}

	next, inserts, ok := e.apply(c, b)
	if !ok {
		return e.done(Consumed, b)
	}

	if inserts {
		e.startInsert(b, next, &change{cmd: c, inserts: true})

		return e.done(Consumed, next)
	}

	next = e.settle(next)
	if next.Text != b.Text {
		e.record(b)
		e.repeat = &change{cmd: c}
	}

	return e.done(Consumed, next)
}

// edge is where a bare j or k goes when there is no line that way to move to.
func (e *Engine) edge(c command, b Buffer) (Outcome, bool) {
	r := []rune(b.Text)
	line := lineOf(r, b.Cursor)

	switch {
	case c.name == "j" && (e.singleLine || line == lastLine(r)):
		return EdgeDown, true
	case c.name == "k" && (e.singleLine || line == 0):
		return EdgeUp, true
	}

	return Consumed, false
}

func step(from, to *[]Buffer, b Buffer) Buffer {
	if len(*from) == 0 {
		return b
	}

	prev := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	*to = append(*to, b)

	return prev
}

func (e *Engine) startInsert(before, at Buffer, recording *change) {
	e.mode = Insert
	e.before, e.typedFrom = before, at
	e.recording = recording
}

func (e *Engine) leaveInsert(b Buffer) Result {
	e.mode = Normal
	e.pending = nil

	if b.Text != e.before.Text {
		e.record(e.before)
	}

	if e.recording != nil {
		e.recording.inserted = typed(e.typedFrom.Text, b.Text)
		e.repeat = e.recording
		e.recording = nil
	}

	r := []rune(b.Text)
	if b.Cursor > lineStart(r, b.Cursor) {
		b.Cursor--
	}

	return e.done(Consumed, e.settle(b))
}

// typed is the text an insert added, found between what the two buffers share at each end.
func typed(from, to string) string {
	a, b := []rune(from), []rune(to)

	head := 0
	for head < len(a) && head < len(b) && a[head] == b[head] {
		head++
	}

	tail := 0
	for tail < len(a)-head && tail < len(b)-head && a[len(a)-1-tail] == b[len(b)-1-tail] {
		tail++
	}

	return string(b[head : len(b)-tail])
}

func (e *Engine) replay(c command, b Buffer) Result {
	if e.repeat == nil {
		return e.done(Consumed, b)
	}

	cmd := e.repeat.cmd
	if c.count > 0 {
		cmd.count, cmd.opCount = c.count, 0
	}

	next, inserts, ok := e.apply(cmd, b)
	if !ok {
		return e.done(Consumed, b)
	}

	if inserts {
		r := []rune(next.Text)
		text := []rune(e.repeat.inserted)
		if e.singleLine {
			text = []rune(strings.ReplaceAll(string(text), "\n", " "))
		}
		r = splice(r, next.Cursor, next.Cursor, text)
		next = Buffer{Text: string(r), Cursor: next.Cursor + len(text)}
		if next.Cursor > lineStart(r, next.Cursor) {
			next.Cursor--
		}
	}

	next = e.settle(next)
	if next.Text != b.Text {
		e.record(b)
	}

	return e.done(Consumed, next)
}

// settle holds a normal-mode cursor on a character of its line.
func (e *Engine) settle(b Buffer) Buffer {
	b = clamp(b)
	r := []rune(b.Text)
	if b.Cursor >= len(r) {
		b.Cursor = max(len(r)-1, 0)
	}
	if b.Cursor < len(r) && r[b.Cursor] == '\n' && b.Cursor > lineStart(r, b.Cursor) {
		b.Cursor--
	}

	return b
}

func clamp(b Buffer) Buffer {
	n := len([]rune(b.Text))
	b.Cursor = max(0, min(b.Cursor, n))

	return b
}

// apply runs a command on b, reporting whether it leaves the engine in insert mode.
func (e *Engine) apply(c command, b Buffer) (Buffer, bool, bool) {
	r := []rune(b.Text)
	p := b.Cursor

	switch c.name {
	case "x":
		return e.apply(command{count: c.times(), op: "d", name: "l"}, b)
	case "X":
		return e.apply(command{count: c.times(), op: "d", name: "h"}, b)
	case "s":
		return e.apply(command{count: c.times(), op: "c", name: "l"}, b)
	case "S":
		return e.apply(command{count: c.times(), op: "c", name: "line"}, b)
	case "D":
		return e.apply(command{count: c.times(), op: "d", name: "$"}, b)
	case "C":
		return e.apply(command{count: c.times(), op: "c", name: "$"}, b)
	case "Y":
		return e.apply(command{count: c.times(), op: "y", name: "line"}, b)
	case "i", "a", "I", "A", "o", "O":
		return e.enter(c.name, r, p)
	case "r":
		return e.replace(c, r, p)
	case "~":
		end := lineEnd(r, p)
		to := min(p+c.times(), end)
		for i := p; i < to; i++ {
			r[i] = toggleCase(r[i])
		}

		return Buffer{Text: string(r), Cursor: to}, false, to > p
	case "p", "P":
		return e.put(c, r, p)
	case "J":
		return e.join(c, r, p)
	}

	if c.op != "" {
		return e.operate(c, r, p)
	}

	to, _, ok := e.motion(r, p, c)

	return Buffer{Text: b.Text, Cursor: to}, false, ok
}

func (e *Engine) enter(name string, r []rune, p int) (Buffer, bool, bool) {
	start, end := lineStart(r, p), lineEnd(r, p)

	switch name {
	case "a":
		if p < end {
			p++
		}
	case "I":
		p = firstNonBlank(r, p)
	case "A":
		p = end
	case "o":
		if e.singleLine {
			return Buffer{}, false, false
		}
		r = splice(r, end, end, []rune{'\n'})
		p = end + 1
	case "O":
		if e.singleLine {
			return Buffer{}, false, false
		}
		r = splice(r, start, start, []rune{'\n'})
		p = start
	}

	return Buffer{Text: string(r), Cursor: p}, true, true
}

func (e *Engine) replace(c command, r []rune, p int) (Buffer, bool, bool) {
	count := c.times()
	if p+count > lineEnd(r, p) {
		return Buffer{}, false, false
	}

	for i := p; i < p+count; i++ {
		r[i] = c.char
	}

	return Buffer{Text: string(r), Cursor: p + count - 1}, false, true
}

func (e *Engine) put(c command, r []rune, p int) (Buffer, bool, bool) {
	text := e.register.text
	if text == "" && !e.register.linewise {
		return Buffer{}, false, false
	}

	if e.register.linewise && !e.singleLine {
		lines := []rune(strings.Repeat(text+"\n", c.times()))
		at := lineEnd(r, p) + 1
		if c.name == "P" {
			at = lineStart(r, p)
		}
		if at > len(r) {
			r = append(r, '\n')
			lines = lines[:len(lines)-1]
		}
		r = splice(r, at, at, lines)

		return Buffer{Text: string(r), Cursor: firstNonBlank(r, at)}, false, true
	}

	if e.singleLine {
		text = strings.ReplaceAll(text, "\n", " ")
	}

	runes := []rune(strings.Repeat(text, c.times()))
	at := p
	if c.name == "p" && p < lineEnd(r, p) {
		at++
	}
	r = splice(r, at, at, runes)

	return Buffer{Text: string(r), Cursor: at + len(runes) - 1}, false, true
}

func (e *Engine) join(c command, r []rune, p int) (Buffer, bool, bool) {
	if e.singleLine {
		return Buffer{}, false, false
	}

	joined := false
	for range max(c.times(), 2) - 1 {
		end := lineEnd(r, p)
		if end >= len(r) {
			break
		}

		next := end + 1
		for next < len(r) && isBlank(r[next]) {
			next++
		}

		var gap []rune
		if end > lineStart(r, end) && !isBlank(r[end-1]) && next < len(r) && r[next] != '\n' && r[next] != ')' {
			gap = []rune{' '}
		}
		r = splice(r, end, next, gap)
		p = end
		joined = true
	}

	return Buffer{Text: string(r), Cursor: p}, false, joined
}

// operate runs d, c or y over the range a motion or text object covers from p.
func (e *Engine) operate(c command, r []rune, p int) (Buffer, bool, bool) {
	var (
		from, to int
		lines    bool
	)

	switch {
	case c.name == "line":
		line := lineOf(r, p)
		last := min(line+c.times()-1, lastLine(r))
		from, to, lines = lineStartOf(r, line), lineEnd(r, lineStartOf(r, last)), true
	case c.object:
		start, end, ok := object(r, p, c.name)
		if !ok {
			return Buffer{}, false, false
		}
		from, to = start, end
	default:
		target, reach, ok := e.motion(r, p, c)
		if !ok {
			return Buffer{}, false, false
		}

		from, to = min(p, target), max(p, target)
		switch reach {
		case inclusive:
			to = min(to+1, len(r))
		case linewise:
			from, to, lines = lineStart(r, from), lineEnd(r, to), true
		case exclusive:
		}
	}

	if from == to && !lines {
		if c.op != "c" {
			return Buffer{}, false, false
		}

		return Buffer{Text: string(r), Cursor: from}, true, true
	}

	e.register.text, e.register.linewise = string(r[from:to]), lines

	switch c.op {
	case "y":
		cursor := from
		if lines && from == lineStart(r, p) {
			cursor = p
		}

		return Buffer{Text: string(r), Cursor: cursor}, false, true
	case "c":
		r = splice(r, from, to, nil)

		return Buffer{Text: string(r), Cursor: from}, true, true
	}

	if !lines {
		r = splice(r, from, to, nil)

		return Buffer{Text: string(r), Cursor: from}, false, true
	}

	switch {
	case to < len(r):
		to++
	case from > 0:
		from--
	}
	r = splice(r, from, to, nil)

	return Buffer{Text: string(r), Cursor: firstNonBlank(r, min(from, len(r)))}, false, true
}
