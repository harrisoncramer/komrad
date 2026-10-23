package paint

import (
	"fmt"
	"slices"
	"strings"
	"sync"
)

// maxDocRenders is how many renders a palette keeps before it forgets them all.
const maxDocRenders = 64

// docRenders is the rows a palette has rendered markdown to.
type docRenders struct {
	mu   sync.Mutex
	rows map[docRenderKey][]string
}

// docRenderKey is one markdown rendered at one width.
type docRenderKey struct {
	markdown string
	width    int
}

// DocRows renders markdown into a column of its own, a row at a time.
func (p Palette) DocRows(markdown string, width int) []string {
	key := docRenderKey{markdown: markdown, width: width}
	if rows, ok := p.renders.get(key); ok {
		return rows
	}

	rows := strings.Split(strings.TrimRight(p.Render(markdown, width), "\n"), "\n")
	p.renders.keep(key, rows)

	return slices.Clone(rows)
}

// DocumentMarkdown is the markdown of a titled page with an optional notice, prose and footer.
func DocumentMarkdown(title, notice, prose, footer string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	if notice != "" {
		b.WriteString(notice + "\n\n")
	}
	if prose != "" {
		b.WriteString(prose + "\n")
	}
	if footer != "" {
		b.WriteString("\n" + footer + "\n")
	}

	return b.String()
}

// get is a copy of the rows kept for a render.
func (r *docRenders) get(key docRenderKey) ([]string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rows, ok := r.rows[key]

	return slices.Clone(rows), ok
}

// keep holds the rows of a render, forgetting every other once it holds as many as it may.
func (r *docRenders) keep(key docRenderKey, rows []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.rows) >= maxDocRenders {
		clear(r.rows)
	}
	r.rows[key] = rows
}
