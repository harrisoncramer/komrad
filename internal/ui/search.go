package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentv1 "claude-agents/proto/gen/agent/v1"
	paginationv1 "claude-agents/proto/gen/pagination/v1"

	"claude-agents/internal/store/transcript"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"google.golang.org/protobuf/proto"
)

// searchLimit caps how many hits a query returns, keeping the result list short
// enough to scan at a glance. The screen shows one page and offers no way to ask
// for another, so the returned page token is ignored.
const searchLimit = 10

// searchDebounce is how long typing has to pause before the query runs. Every
// keystroke used to be its own round trip, most of them for a prefix nobody wanted
// results for, so the wait is long enough to swallow a burst of typing and short
// enough that a pause still feels answered immediately.
const searchDebounce = 120 * time.Millisecond

// matchStyle highlights the matched terms inside a result row's snippet, the
// panel-side rendering of the markers the store wraps hits in.
var matchStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF9E3B")).Bold(true)

// searchHit is one result of a transcript search: the turn that matched, the agent
// it belongs to, a snippet of the matching text with the match wrapped in the
// store's markers, and where the turn sits in the full transcript so the preview
// can scroll to it.
type searchHit struct {
	agentID string
	name    string
	deleted bool
	offset  int64
	snippet string
}

// searchSettledMsg fires once typing has paused for searchDebounce. The query it
// carries is the one that was current when the pause began, so a keystroke landing
// during the wait leaves it stale and it is dropped rather than queried for. Like the
// refresh tick it is a prompt to go and ask rather than an answer, which is what its
// suffix says.
type searchSettledMsg struct {
	query string
}

// searchResult carries the hits for a query back to the update loop. The
// query is echoed so a late response for a query the user has since edited past
// can be discarded rather than shown against the wrong input.
type searchResult struct {
	query string
	hits  []searchHit
}

// previewResult carries a hovered hit's full transcript back for the
// preview. The agent id and query are echoed so a response arriving after the
// hover or the query moved on is discarded rather than staged over the wrong hit.
// The offset is the hit's own, so the preview centres on the turn that matched
// rather than wherever the words first happen to appear.
type previewResult struct {
	agentID string
	query   string
	offset  int64
	content string
}

// enterSearch switches to the search screen with an empty query, blanking the
// stage until the first result is hovered.
func (m model) enterSearch() (tea.Model, tea.Cmd) {
	m.screen = screenSearch
	m.searchQuery = ""
	m.searchHits = nil
	m.searchCursor = 0
	m.stage.Blank()

	return m, nil
}

// debounceSearchCmd waits out searchDebounce before the query is run, so a burst
// of typing costs one request rather than one per key. A blank query clears the
// results straight away, since there is nothing to wait to ask for.
func (m model) debounceSearchCmd(query string) tea.Cmd {
	if strings.TrimSpace(query) == "" {
		return func() tea.Msg { return searchResult{query: query} }
	}

	return tea.Tick(searchDebounce, func(time.Time) tea.Msg {
		return searchSettledMsg{query: query}
	})
}

// runSettledSearch runs a debounced query, unless the user typed on during the
// wait, in which case the keystroke that did so has its own pause to sit out.
func (m model) runSettledSearch(msg searchSettledMsg) tea.Cmd {
	if msg.query != m.searchQuery {
		return nil
	}

	return m.searchCmd(msg.query)
}

// searchCmd runs the query over the service off the UI goroutine, so typing
// never blocks on the RPC. A blank query resolves to no hits without a call.
func (m model) searchCmd(query string) tea.Cmd {
	client := m.api.agents

	return func() tea.Msg {
		if strings.TrimSpace(query) == "" {
			return searchResult{query: query}
		}

		ctx, cancel := context.WithTimeout(context.Background(), agentsTimeout)
		defer cancel()

		resp, err := client.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
			Query:      query,
			Pagination: &paginationv1.Pagination{Limit: proto.Int32(searchLimit)},
		}))
		if err != nil {
			return searchResult{query: query}
		}

		hits := make([]searchHit, 0, len(resp.Msg.GetHits()))
		for _, h := range resp.Msg.GetHits() {
			hits = append(hits, searchHit{
				agentID: h.GetAgentId(),
				name:    h.GetName(),
				deleted: h.GetDeleted(),
				offset:  h.GetContentOffset(),
				snippet: h.GetSnippet(),
			})
		}

		return searchResult{query: query, hits: hits}
	}
}

// applySearch installs hits for the current query and returns a command to
// load the hovered hit's transcript preview. A result for a stale query is ignored
// so the list never shows hits for text the user has already changed.
func (m *model) applySearch(result searchResult) tea.Cmd {
	if result.query != m.searchQuery {
		return nil
	}

	m.searchHits = result.hits
	m.searchCursor = clampCursor(m.searchCursor, len(m.searchHits))
	if len(m.searchHits) == 0 {
		m.stage.Blank()
		return nil
	}

	return m.previewCmd(m.searchHits[m.searchCursor])
}

// previewCmd loads a hit's full transcript off the UI goroutine, so hovering a
// result never blocks the render loop on the fetch.
func (m model) previewCmd(hit searchHit) tea.Cmd {
	client := m.api.agents
	query := m.searchQuery

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), agentsTimeout)
		defer cancel()

		resp, err := client.GetTranscript(ctx, connect.NewRequest(&agentv1.GetTranscriptRequest{
			AgentId: hit.agentID,
		}))
		if err != nil {
			return previewResult{agentID: hit.agentID, query: query, offset: hit.offset}
		}

		return previewResult{
			agentID: hit.agentID,
			query:   query,
			offset:  hit.offset,
			content: resp.Msg.GetContent(),
		}
	}
}

// applyPreview stages a loaded transcript centered on the match, ignoring a
// response that no longer matches the hovered hit or the current query.
func (m *model) applyPreview(result previewResult) {
	if result.query != m.searchQuery {
		return
	}
	if m.searchCursor < 0 || m.searchCursor >= len(m.searchHits) {
		return
	}
	if m.searchHits[m.searchCursor].agentID != result.agentID {
		return
	}

	m.stage.Transcript(result.content, lineAtOffset(result.content, result.offset))
}

// lineAtOffset returns the 1-based line the given byte offset falls on, so the
// preview can centre on the turn that matched. Matching is stemmed, so the words the
// user typed need not appear in the text they found. The offset was recorded when
// the turn was indexed and locates it either way. An offset past the end of the
// content means the two have drifted, which anchors at the top rather than guessing.
func lineAtOffset(content string, offset int64) int {
	if offset < 0 || offset > int64(len(content)) {
		return 0
	}

	return 1 + strings.Count(content[:offset], "\n")
}

// updateSearch drives the search screen. Printable keys edit the query and re-run
// the search, the nav keys move over the results, enter opens the hovered agent,
// and esc returns to the list. Exit uses fixed keys rather than the configurable
// agent bindings, whose letters would otherwise type into the query.
func (m model) updateSearch(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case k == "esc":
		m.screen = screenAgents
		m.stageSelection()
		return m, nil
	case k == "ctrl+c":
		return m, m.hide()
	case bound(k, m.keys.Nav.Up):
		if m.searchCursor > 0 {
			m.searchCursor--
			return m, m.previewCmd(m.searchHits[m.searchCursor])
		}
		return m, nil
	case bound(k, m.keys.Nav.Down):
		if m.searchCursor < len(m.searchHits)-1 {
			m.searchCursor++
			return m, m.previewCmd(m.searchHits[m.searchCursor])
		}
		return m, nil
	case k == "enter":
		return m.openSearchHit()
	case k == "backspace":
		if m.searchQuery != "" {
			m.searchQuery = trimLastRune(m.searchQuery)
		}
		return m, m.debounceSearchCmd(m.searchQuery)
	case key.Type == tea.KeySpace:
		m.searchQuery += " "
		return m, m.debounceSearchCmd(m.searchQuery)
	case key.Type == tea.KeyRunes:
		m.searchQuery += string(key.Runes)
		return m, m.debounceSearchCmd(m.searchQuery)
	}

	return m, nil
}

// openSearchHit opens the hovered result's agent and returns to the list focused
// on it. Restoring a dormant or deleted agent happens in the manager, so this
// just stages the resulting pane.
func (m model) openSearchHit() (tea.Model, tea.Cmd) {
	if m.searchCursor < 0 || m.searchCursor >= len(m.searchHits) {
		return m, nil
	}

	hit := m.searchHits[m.searchCursor]

	// Opening a hit is a restore. The agent behind it may be archived or dormant,
	// and a live one comes back as it stands rather than being started twice.
	return m, m.restoreCmd(fmt.Sprintf("Could not restore agent %q", hit.name), hit.agentID, screenAgents)
}

// searchView renders the search screen's own pane. The query line sits above the
// list of matching agents, each with a one-line snippet of its match.
func (m model) searchView() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("Search") + "\n\n")
	b.WriteString(cursorStyle.Render("/") + " " + m.searchQuery + cursorStyle.Render("▏") + "\n\n")

	switch {
	case strings.TrimSpace(m.searchQuery) == "":
		b.WriteString(dimStyle.Render("type to search transcripts") + "\n")
	case len(m.searchHits) == 0:
		b.WriteString(dimStyle.Render("no matches") + "\n")
	default:
		// One conversation matches once per turn that did, so its hits arrive as
		// several adjacent rows. Repeating the name down all of them reads as
		// several agents, so only the first of a run is labelled and the rest sit
		// under it as further places the same conversation matched.
		labelled := ""
		for i, hit := range m.searchHits {
			if hit.name != labelled {
				name := hit.name
				if hit.deleted {
					name += dimStyle.Render(" (deleted)")
				}
				b.WriteString("  " + name + "\n")
				labelled = hit.name
			}

			prefix := "    "
			if i == m.searchCursor {
				prefix = "  " + cursorStyle.Render("› ")
			}

			b.WriteString(prefix + dimStyle.Render(renderSnippet(hit.snippet, m.width-snippetIndent)) + "\n")
		}
	}

	b.WriteString("\n" + m.searchHints())

	return b.String()
}

// searchHints lists the search screen's fixed key hints.
func (m model) searchHints() string {
	hints := []struct{ key, desc string }{
		{"enter", "open"},
		{hintKeys(m.keys.Nav.Up) + "/" + hintKeys(m.keys.Nav.Down), "move"},
		{"esc", "back"},
	}

	keyWidth := 0
	for _, h := range hints {
		if w := lipgloss.Width(h.key); w > keyWidth {
			keyWidth = w
		}
	}
	keyStyle := lipgloss.NewStyle().Width(keyWidth + 1)

	var b strings.Builder
	for _, h := range hints {
		b.WriteString(dimStyle.Render(keyStyle.Render(h.key) + h.desc))
		b.WriteString("\n")
	}

	return b.String()
}

// snippetIndent is how much of a result row the cursor column and indent take, the
// part of the pane a snippet cannot have.
const snippetIndent = 4

// snippetClipLead is how many characters of a clipped snippet are kept ahead of its
// match, enough that the match reads in context instead of starting the line.
const snippetClipLead = 12

// clipToMatch cuts a snippet down to width visible characters, keeping the first
// marked match within what survives. A snippet that already fits, or a width from a
// pane nothing has measured yet, is returned whole. The markers are not visible and
// so do not count against the width, and an ellipsis stands in at either end that
// was cut.
//
// Without this a long snippet is simply too wide for the pane, and since the match
// can sit anywhere inside it, the part that gets cut off is often the match itself.
func clipToMatch(snippet string, width int) string {
	if width <= 0 {
		return snippet
	}

	const ellipsis = "…"

	openMark, closeMark := rune(transcript.SnippetOpen[0]), rune(transcript.SnippetClose[0])
	runes := []rune(snippet)

	visible, matchAt := 0, -1
	for _, r := range runes {
		switch r {
		case openMark:
			if matchAt < 0 {
				matchAt = visible
			}
		case closeMark:
		default:
			visible++
		}
	}
	if visible <= width {
		return snippet
	}

	// Both ellipses come out of the width, since a row that overruns the pane is the
	// thing being fixed.
	budget := max(width-2*len(ellipsis), 1)

	start := 0
	if matchAt > snippetClipLead {
		start = matchAt - snippetClipLead
	}
	if start+budget > visible {
		start = visible - budget
	}

	var (
		b       strings.Builder
		at      int
		marked  bool
		wrapped bool
	)
	if start > 0 {
		b.WriteString(ellipsis)
	}

	for _, r := range runes {
		switch r {
		case openMark:
			marked = true
			if at >= start && at < start+budget {
				b.WriteRune(r)
				wrapped = true
			}

			continue
		case closeMark:
			marked = false
			if wrapped {
				b.WriteRune(r)
				wrapped = false
			}

			continue
		}

		if at >= start && at < start+budget {
			// The window opened partway through a marked run, so the marker that began
			// it fell outside it and is reissued here.
			if marked && !wrapped {
				b.WriteString(transcript.SnippetOpen)
				wrapped = true
			}
			b.WriteRune(r)
		}
		at++
	}

	if wrapped {
		b.WriteString(transcript.SnippetClose)
	}
	if start+budget < visible {
		b.WriteString(ellipsis)
	}

	return b.String()
}

// renderSnippet styles a hit's snippet for the list, turning the store's match
// markers into a highlighted run, collapsing newlines so the snippet stays on one
// line, and clipping it to width so it cannot overrun the pane.
func renderSnippet(s string, width int) string {
	s = clipToMatch(strings.ReplaceAll(s, "\n", " "), width)

	var b strings.Builder
	for {
		i := strings.Index(s, transcript.SnippetOpen)
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])

		rest := s[i+len(transcript.SnippetOpen):]
		j := strings.Index(rest, transcript.SnippetClose)
		if j < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(matchStyle.Render(rest[:j]))
		s = rest[j+len(transcript.SnippetClose):]
	}

	return b.String()
}

// trimLastRune drops the final rune of s, so backspace deletes a whole
// multi-byte character rather than one byte of it.
func trimLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}

	return string(r[:len(r)-1])
}
