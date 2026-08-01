package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentv1 "weave/proto/gen/agent/v1"
	paginationv1 "weave/proto/gen/pagination/v1"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"
)

// searchState is the search screen, the live query with its hits and which one is
// hovered.
type searchState struct {
	query  string
	hits   []searchHit
	cursor int
}

// searchLimit caps how many hits a query returns. The screen shows one page and offers no
// way to ask for another, so the returned page token is ignored.
const searchLimit = 10

// searchDebounce is how long typing has to pause before the query runs, long enough to
// swallow a burst and short enough that a pause still feels answered immediately.
const searchDebounce = 120 * time.Millisecond

// searchHit is one result of a transcript search. The snippet carries the match wrapped in
// the store's markers, and offset is where the turn sits in the full transcript so the
// preview can scroll to it.
type searchHit struct {
	agentID  string
	name     string
	archived bool
	offset   int64
	snippet  string
}

// searchSettledMsg fires once typing has paused for searchDebounce, carrying the query
// current when the pause began, so a keystroke during the wait leaves it stale and it is
// dropped rather than queried for.
type searchSettledMsg struct {
	query string
}

// searchResult carries the hits for a query back to the update loop, echoing the query so
// a late response is discarded rather than shown against the wrong input.
type searchResult struct {
	query string
	hits  []searchHit
}

// previewResult carries a hovered hit's full transcript back for the preview. The agent id
// and query are echoed so a response arriving after either moved on is discarded rather
// than staged over the wrong hit, and the offset is the hit's own, so the preview centres
// on the turn that matched rather than wherever the words first appear.
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
	m.search.query = ""
	m.search.hits = nil
	m.search.cursor = 0
	m.stage.Blank()

	return m, nil
}

// debounceSearchCmd waits out searchDebounce before the query runs, so a burst of typing
// costs one request rather than one per key. A blank query clears the results at once.
func (m model) debounceSearchCmd(query string) tea.Cmd {
	if strings.TrimSpace(query) == "" {
		return func() tea.Msg { return searchResult{query: query} }
	}

	return tea.Tick(searchDebounce, func(time.Time) tea.Msg {
		return searchSettledMsg{query: query}
	})
}

// runSettledSearch runs a debounced query, unless the user typed on during the wait, in
// which case that keystroke has its own pause to sit out.
func (m model) runSettledSearch(msg searchSettledMsg) tea.Cmd {
	if msg.query != m.search.query {
		return nil
	}

	return m.searchCmd(msg.query)
}

// searchCmd runs the query over the service off the UI goroutine, so typing never blocks
// on the RPC. A blank query resolves to no hits without a call.
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
				agentID:  h.GetAgentId(),
				name:     h.GetName(),
				archived: h.GetArchived(),
				offset:   h.GetContentOffset(),
				snippet:  h.GetSnippet(),
			})
		}

		return searchResult{query: query, hits: hits}
	}
}

// applySearch installs hits for the current query and returns a command to load the
// hovered hit's transcript preview. A result for a stale query is ignored.
func (m *model) applySearch(result searchResult) tea.Cmd {
	if result.query != m.search.query {
		return nil
	}

	m.search.hits = result.hits
	m.search.cursor = clampCursor(m.search.cursor, len(m.search.hits))
	if len(m.search.hits) == 0 {
		m.stage.Blank()
		return nil
	}

	return m.previewCmd(m.search.hits[m.search.cursor])
}

// previewCmd loads a hit's full transcript off the UI goroutine, so hovering a result
// never blocks the render loop on the fetch.
func (m model) previewCmd(hit searchHit) tea.Cmd {
	client := m.api.agents
	query := m.search.query

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
	if result.query != m.search.query {
		return
	}
	if m.search.cursor < 0 || m.search.cursor >= len(m.search.hits) {
		return
	}
	if m.search.hits[m.search.cursor].agentID != result.agentID {
		return
	}

	m.stage.Transcript(result.content, lineAtOffset(result.content, result.offset))
}

// updateSearch drives the search screen. Every key here is fixed rather than one of the
// configurable bindings, whose letters would otherwise type into the query.
func (m model) updateSearch(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case k == "esc":
		m.screen = screenAgents
		m.stageSelection()
		return m, nil
	case k == "ctrl+c":
		return m, m.hide()
	case k == "up" || k == "ctrl+p":
		if m.search.cursor > 0 {
			m.search.cursor--
			return m, m.previewCmd(m.search.hits[m.search.cursor])
		}
		return m, nil
	case k == "down" || k == "ctrl+n":
		if m.search.cursor < len(m.search.hits)-1 {
			m.search.cursor++
			return m, m.previewCmd(m.search.hits[m.search.cursor])
		}
		return m, nil
	case k == "enter":
		return m.openSearchHit()
	case k == "backspace":
		if m.search.query != "" {
			m.search.query = trimLastRune(m.search.query)
		}
		return m, m.debounceSearchCmd(m.search.query)
	case key.Type == tea.KeySpace:
		m.search.query += " "
		return m, m.debounceSearchCmd(m.search.query)
	case key.Type == tea.KeyRunes:
		m.search.query += string(key.Runes)
		return m, m.debounceSearchCmd(m.search.query)
	}

	return m, nil
}

// openSearchHit opens the hovered result's agent and lands on the tree focused on it. An
// archived hit is restored on the way, since search is the one screen that reaches those
// conversations.
func (m model) openSearchHit() (tea.Model, tea.Cmd) {
	if m.search.cursor < 0 || m.search.cursor >= len(m.search.hits) {
		return m, nil
	}

	hit := m.search.hits[m.search.cursor]

	return m, m.resumeCmd(resumeParams{
		summary:        fmt.Sprintf("Could not open agent %q", hit.name),
		id:             hit.agentID,
		unarchiveFirst: hit.archived,
	})
}

// searchView renders the search screen's own pane. The query line sits above the list of
// matching agents, each with a one-line snippet of its match.
func (m model) searchView() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("Search") + "\n\n")
	b.WriteString(cursorStyle.Render("/") + " " + m.search.query + cursorStyle.Render("▏") + "\n\n")

	switch {
	case strings.TrimSpace(m.search.query) == "":
		b.WriteString(dimStyle.Render("type to search transcripts") + "\n")
	case len(m.search.hits) == 0:
		b.WriteString(dimStyle.Render("no matches") + "\n")
	default:
		// One conversation matches once per turn that did, so its hits arrive as several
		// adjacent rows and only the first of a run is labelled, or it would read as
		// several agents.
		labelled := ""
		for i, hit := range m.search.hits {
			if hit.name != labelled {
				name := hit.name
				if hit.archived {
					name += dimStyle.Render(" (archived)")
				}
				b.WriteString("  " + name + "\n")
				labelled = hit.name
			}

			prefix := "    "
			if i == m.search.cursor {
				prefix = "  " + cursorStyle.Render("› ")
			}

			b.WriteString(prefix + dimStyle.Render(renderSnippet(hit.snippet, m.width-snippetIndent)) + "\n")
		}
	}

	b.WriteString("\n" + m.renderHelp(m.searchHelp()))

	return b.String()
}
