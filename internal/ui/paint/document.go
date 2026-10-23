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
	rows map[docRender][]string
}

// docRender is one markdown rendered at one width.
type docRender struct {
	markdown string
	width    int
}

// DocRows renders markdown into a column of its own, a row at a time.
func (p Palette) DocRows(markdown string, width int) []string {
	key := docRender{markdown: markdown, width: width}

	p.renders.mu.Lock()
	rows, ok := p.renders.rows[key]
	p.renders.mu.Unlock()
	if ok {
		return slices.Clone(rows)
	}

	rows = strings.Split(strings.TrimRight(renderWith(markdown, width, p.Stage()), "\n"), "\n")

	p.renders.mu.Lock()
	if len(p.renders.rows) >= maxDocRenders {
		clear(p.renders.rows)
	}
	p.renders.rows[key] = rows
	p.renders.mu.Unlock()

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
