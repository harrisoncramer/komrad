package ui

import (
	"context"
	"strings"

	"claude-agents/internal/store"
	paginationv1 "claude-agents/proto/gen/pagination/v1"
	sessionv1 "claude-agents/proto/gen/session/v1"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"google.golang.org/protobuf/proto"
)

// searchLimit caps how many hits a query returns, keeping the result list short
// enough to scan at a glance. The screen shows one page and offers no way to ask
// for another, so the returned page token is ignored.
const searchLimit = 10

// matchStyle highlights the matched terms inside a result row's snippet, the
// panel-side rendering of the markers the store wraps hits in.
var matchStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF9E3B")).Bold(true)

// searchHit is one result of a transcript search: the chat it points at and a
// snippet of the matching text with the match wrapped in the store's markers.
type searchHit struct {
	sessionID string
	name      string
	deleted   bool
	snippet   string
}

// searchResultsMsg carries the hits for a query back to the update loop. The
// query is echoed so a late response for a query the user has since edited past
// can be discarded rather than shown against the wrong input.
type searchResultsMsg struct {
	query string
	hits  []searchHit
}

// transcriptLoadedMsg carries a hovered hit's full transcript back for the
// preview. The session id and query are echoed so a response that arrives after
// the hover or the query moved on is discarded rather than staged over the wrong
// hit.
type transcriptLoadedMsg struct {
	sessionID string
	query     string
	content   string
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

// searchCmd runs the query over the service off the UI goroutine, so typing
// never blocks on the RPC. A blank query resolves to no hits without a call.
func (m model) searchCmd(query string) tea.Cmd {
	client := m.api.sessions

	return func() tea.Msg {
		if strings.TrimSpace(query) == "" {
			return searchResultsMsg{query: query}
		}

		ctx, cancel := context.WithTimeout(context.Background(), chatsTimeout)
		defer cancel()

		resp, err := client.SearchTranscripts(ctx, connect.NewRequest(&sessionv1.SearchTranscriptsRequest{
			Query:      query,
			Pagination: &paginationv1.Pagination{Limit: proto.Int32(searchLimit)},
		}))
		if err != nil {
			return searchResultsMsg{query: query}
		}

		hits := make([]searchHit, 0, len(resp.Msg.GetHits()))
		for _, h := range resp.Msg.GetHits() {
			hits = append(hits, searchHit{
				sessionID: h.GetSessionId(),
				name:      h.GetName(),
				deleted:   h.GetDeleted(),
				snippet:   h.GetSnippet(),
			})
		}

		return searchResultsMsg{query: query, hits: hits}
	}
}

// applySearchResults installs hits for the current query and returns a command to
// load the hovered hit's transcript preview. A result for a stale query is ignored
// so the list never shows hits for text the user has already changed.
func (m *model) applySearchResults(msg searchResultsMsg) tea.Cmd {
	if msg.query != m.searchQuery {
		return nil
	}

	m.searchHits = msg.hits
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
	client := m.api.sessions
	query := m.searchQuery

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), chatsTimeout)
		defer cancel()

		resp, err := client.GetTranscript(ctx, connect.NewRequest(&sessionv1.GetTranscriptRequest{
			SessionId: hit.sessionID,
		}))
		if err != nil {
			return transcriptLoadedMsg{sessionID: hit.sessionID, query: query}
		}

		return transcriptLoadedMsg{sessionID: hit.sessionID, query: query, content: resp.Msg.GetContent()}
	}
}

// applyTranscript stages a loaded transcript centered on the match, ignoring a
// response that no longer matches the hovered hit or the current query.
func (m *model) applyTranscript(msg transcriptLoadedMsg) {
	if msg.query != m.searchQuery {
		return
	}
	if m.searchCursor < 0 || m.searchCursor >= len(m.searchHits) {
		return
	}
	if m.searchHits[m.searchCursor].sessionID != msg.sessionID {
		return
	}

	m.stage.Transcript(msg.content, firstMatchLine(msg.content, m.searchQuery))
}

// firstMatchLine returns the 1-based line of the first case-insensitive
// occurrence of query in content, or 0 when it does not occur, so the preview
// can center on the match. The store search is a substring match, so the query
// text itself appears in the content it matched.
func firstMatchLine(content, query string) int {
	query = strings.TrimSpace(query)
	if query == "" {
		return 0
	}

	idx := strings.Index(strings.ToLower(content), strings.ToLower(query))
	if idx < 0 {
		return 0
	}

	return 1 + strings.Count(content[:idx], "\n")
}

// updateSearch drives the search screen. Printable keys edit the query and re-run
// the search, the nav keys move over the results, enter opens the hovered chat,
// and esc returns to the list. Movement and exit use fixed keys rather than the
// configurable chat bindings, whose letters would otherwise type into the query.
func (m model) updateSearch(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case k == "esc":
		m.screen = screenChats
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
		return m, m.searchCmd(m.searchQuery)
	case key.Type == tea.KeySpace:
		m.searchQuery += " "
		return m, m.searchCmd(m.searchQuery)
	case key.Type == tea.KeyRunes:
		m.searchQuery += string(key.Runes)
		return m, m.searchCmd(m.searchQuery)
	}

	return m, nil
}

// openSearchHit opens the hovered result's chat and returns to the list focused
// on it. Restoring a dormant or deleted chat happens in the manager, so this
// just stages the resulting pane.
func (m model) openSearchHit() (tea.Model, tea.Cmd) {
	if m.searchCursor < 0 || m.searchCursor >= len(m.searchHits) {
		return m, nil
	}

	hit := m.searchHits[m.searchCursor]

	// Opening a hit is a restore: the chat behind it may be archived or dormant,
	// and a live one comes back as it stands rather than being started twice.
	restored, err := m.restoreChat(hit.sessionID)
	if err != nil {
		return m, nil
	}

	m.screen = screenChats
	m.reload()
	m.chatCursor = clampCursor(rowIndexOfChatName(m.chatRows(), restored.GetName()), len(m.chatRows()))
	m.stage.Chat(restored.GetPaneId())

	return m, nil
}

// searchView renders the search screen's own pane. The query line sits above the
// list of matching chats, each with a one-line snippet of its match.
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
		for i, hit := range m.searchHits {
			prefix := "  "
			name := hit.name
			if i == m.searchCursor {
				prefix = cursorStyle.Render("› ")
				name = cursorStyle.Render(name)
			}
			if hit.deleted {
				name += dimStyle.Render(" (deleted)")
			}

			b.WriteString(prefix + name + "\n")
			b.WriteString("    " + dimStyle.Render(renderSnippet(hit.snippet)) + "\n")
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

// renderSnippet styles a hit's snippet for the list, turning the store's match
// markers into a highlighted run and collapsing newlines so the snippet stays on
// one line.
func renderSnippet(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")

	var b strings.Builder
	for {
		i := strings.Index(s, store.SnippetOpen)
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])

		rest := s[i+len(store.SnippetOpen):]
		j := strings.Index(rest, store.SnippetClose)
		if j < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(matchStyle.Render(rest[:j]))
		s = rest[j+len(store.SnippetClose):]
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
