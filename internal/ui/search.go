package ui

import (
	"context"
	"strings"
	"time"

	"weave/internal/ui/layout"
	agentv1 "weave/proto/gen/agent/v1"
	paginationv1 "weave/proto/gen/pagination/v1"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"
)

type searchState struct {
	// typing is whether the query line has the keys, which it takes whole so a letter bound
	// elsewhere still types, the same way the filter's input does.
	typing bool
	// query is what has been typed, kept once the line hands the keys over so the hits below
	// stay put for the keys that act on them.
	query  string
	hits   []searchHit
	cursor int
}

// searchLimit caps how many hits a query returns; the screen shows one page, so the
// returned page token is ignored.
const searchLimit = 10

// searchDebounce is how long typing has to pause before the query runs, long enough to
// swallow a burst and short enough that a pause still feels answered immediately.
const searchDebounce = 120 * time.Millisecond

// searchHit is one result of a transcript search; snippet carries the match wrapped in the
// store's markers, and offset locates the turn in the full transcript for the preview.
type searchHit struct {
	agentID  string
	name     string
	archived bool
	offset   int64
	snippet  string
}

// searchSettledMsg fires once typing has paused for searchDebounce, carrying the query
// current when the pause began, so a keystroke during the wait leaves it stale and dropped.
type searchSettledMsg struct {
	query string
}

// searchResult carries the hits for a query back to the update loop, echoing the query so
// a late response is discarded rather than shown against the wrong input.
type searchResult struct {
	query string
	hits  []searchHit
}

// previewResult carries a hovered hit's transcript back for the preview; agentID and query
// are echoed to discard stale responses, and offset centers the preview on the matched turn.
type previewResult struct {
	agentID string
	query   string
	offset  int64
	content string
}

func (m model) enterSearch() (tea.Model, tea.Cmd) {
	m.enterScreen(screenSearch)
	m.search = searchState{typing: true}
	m.frame.Show(layout.RegionStage, newBlankContent())

	return m, nil
}

// openSearchInput hands the keys back to the query line, leaving what was typed to be edited
// and the hits below it standing.
func (m model) openSearchInput() (tea.Model, tea.Cmd) {
	m.search.typing = true

	return m, nil
}

// debounceSearchCmd waits out searchDebounce before the query runs, so a burst of typing
// costs one request rather than one per key.
func (m model) debounceSearchCmd(query string) tea.Cmd {
	if strings.TrimSpace(query) == "" {
		return func() tea.Msg { return searchResult{query: query} }
	}

	return tea.Tick(searchDebounce, func(time.Time) tea.Msg {
		return searchSettledMsg{query: query}
	})
}

func (m model) runSettledSearch(msg searchSettledMsg) tea.Cmd {
	if msg.query != m.search.query {
		return nil
	}

	return m.searchCmd(msg.query)
}

// searchCmd runs the query over the service off the UI goroutine, so typing never blocks
// on the RPC.
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

// applySearch installs hits for the current query and loads the hovered hit's preview; a
// result for a stale query is ignored.
func (m *model) applySearch(result searchResult) tea.Cmd {
	if result.query != m.search.query {
		return nil
	}

	m.search.hits = result.hits
	m.search.cursor = clampCursor(m.search.cursor, len(m.search.hits))
	if len(m.search.hits) == 0 {
		m.frame.Show(layout.RegionStage, newBlankContent())
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

// applyPreview ignores a response that no longer matches the hovered hit or the current
// query.
func (m *model) applyPreview(result previewResult) {
	if result.query != m.search.query {
		return
	}

	hit, ok := m.hoveredHit()
	if !ok || hit.agentID != result.agentID {
		return
	}

	m.stageTranscript(result.content, lineAtOffset(result.content, result.offset))
}

// noTranscriptMarkdown is what the stage says for an agent that has not written one yet, so
// the pane answers rather than sitting blank on a hit whose transcript has since gone.
const noTranscriptMarkdown = "_no transcript yet_"

func (m *model) stageTranscript(conversation string, line int) {
	if strings.TrimSpace(conversation) == "" {
		m.frame.Show(layout.RegionStage, newTextContent(noTranscriptMarkdown))

		return
	}

	m.frame.Show(layout.RegionStage, newTranscriptContent(conversation, line))
}

// updateSearch splits the screen the way the filter splits a list: the query line has the
// keys while it is being typed into, and the hits below it have them once it hands them over.
func (m model) updateSearch(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.search.typing {
		return m.updateSearchInput(key)
	}

	return m.updateSearchHits(key)
}

// updateSearchInput drives the query line, which owns every key while it holds them, since a
// configurable letter would otherwise act rather than type. enter hands the keys to the hits
// and esc leaves the screen, there being nothing behind the line to go back to.
func (m model) updateSearchInput(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k := key.String(); {
	case k == "esc":
		m.search = searchState{}

		return m.returnToAgents()
	case k == "enter":
		m.search.typing = false

		return m, nil
	case k == "ctrl+c":
		return m, m.hide()
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

// updateSearchHits drives the results once the query line has let go of the keys, where the
// shared navigation keys run down them and enter lands on the one under the cursor. Both the
// back key and the filter key put the keys back in the line, so a query is edited rather than
// retyped.
func (m model) updateSearchHits(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	if m.takeTopPress(k) {
		return m.placeSearchCursor(0)
	}
	if m.topHeld {
		return m, nil
	}

	switch {
	case k == "esc", isBound(k, m.keys.Filter.Open):
		return m.openSearchInput()
	case k == "ctrl+c":
		return m, m.hide()
	case isBound(k, m.keys.Nav.Up):
		return m.moveSearchCursor(-1)
	case isBound(k, m.keys.Nav.Down):
		return m.moveSearchCursor(1)
	case isBound(k, m.keys.Nav.Bottom):
		return m.placeSearchCursor(len(m.search.hits) - 1)
	case k == "enter":
		return m.jumpToSearchHit()
	}

	return m, nil
}

func (m model) moveSearchCursor(delta int) (tea.Model, tea.Cmd) {
	next, ok := stepCursor(m.search.cursor, delta, len(m.search.hits))
	if !ok {
		return m, nil
	}

	m.search.cursor = next

	return m, m.previewCmd(m.search.hits[next])
}

func (m model) placeSearchCursor(index int) (tea.Model, tea.Cmd) {
	if len(m.search.hits) == 0 {
		return m, nil
	}

	m.search.cursor = clampCursor(index, len(m.search.hits))

	return m, m.previewCmd(m.search.hits[m.search.cursor])
}

func (m model) hoveredHit() (searchHit, bool) {
	return rowAt(m.search.hits, m.search.cursor)
}

// jumpToSearchHit lands on the agents tree for a live conversation or the archive for one
// put away, without resuming or restoring anything, since a search only says where
// something is.
func (m model) jumpToSearchHit() (tea.Model, tea.Cmd) {
	hit, ok := m.hoveredHit()
	if !ok {
		return m, nil
	}

	if hit.archived {
		m.archive.focus = hit.agentID

		return m.enterArchive()
	}

	m.enterScreen(screenAgents)
	m.revealAgent(hit.agentID)
	m.stageSelection()

	return m, nil
}

func (m model) searchView() string {
	var b strings.Builder
	b.WriteString(m.titleStyle().Render("Search") + "\n\n")
	query := cursorStyle.Render("/") + " " + m.search.query
	if m.search.typing {
		query += cursorStyle.Render("▏")
	}
	b.WriteString(query + "\n\n")

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
				name := clipName(hit.name)
				if hit.archived {
					name += dimStyle.Render(" (archived)")
				}
				b.WriteString("  " + name + "\n")
				labelled = hit.name
			}

			prefix := "    "
			if i == m.search.cursor {
				prefix = "  " + m.rowCursorStyle().Render("› ")
			}

			b.WriteString(prefix + dimStyle.Render(renderSnippet(hit.snippet, m.width-snippetIndent)) + "\n")
		}
	}

	b.WriteString("\n" + m.renderHelp(m.searchHelp()))

	return b.String()
}
