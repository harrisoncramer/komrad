package panel

import (
	"strings"

	"claude-agents/internal/agents"
	"claude-agents/internal/search"
	"claude-agents/internal/store"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// searchResultLimit bounds how many ranked matches the screen shows, since a
// short fuzzy query can match nearly every chat.
const searchResultLimit = 30

// indexBuiltMsg delivers the search index once its transcripts have been read
// off the main loop, along with the agents each result maps back to.
type indexBuiltMsg struct {
	index  search.Index
	agents map[string]store.Agent
}

// enterSearch opens the search screen and kicks off building the index in the
// background, so reading transcripts never blocks the first paint. The index is
// scoped to the current workflow, so search inside a workflow matches only its
// chats.
func (m model) enterSearch() (tea.Model, tea.Cmd) {
	m.screen = screenSearch
	m.searching = true
	m.searchResults = nil
	m.searchCursor = 0
	m.searchInput.SetValue("")
	m.searchInput.Focus()

	return m, buildIndexCmd(m.manager, m.workflow)
}

// buildIndexCmd loads the in-scope chats, active and deleted, and distills each
// transcript into the searchable index off the main loop. With a workflow in
// scope it loads only that workflow's chats, otherwise every chat.
func buildIndexCmd(manager *agents.AgentManager, workflow *store.Workflow) tea.Cmd {
	return func() tea.Msg {
		candidates, err := searchCandidates(manager, workflow)
		if err != nil {
			return indexBuiltMsg{agents: map[string]store.Agent{}}
		}

		chats := make([]search.Chat, 0, len(candidates))
		agents := make(map[string]store.Agent, len(candidates))
		for _, agent := range candidates {
			chats = append(chats, search.Chat{
				Name:            agent.Name,
				ClaudeSessionID: agent.ClaudeSessionID,
				Deleted:         agent.Deleted,
			})
			agents[agent.Name] = agent
		}

		return indexBuiltMsg{
			index:  search.BuildIndex(chats, search.DefaultProjectsDir()),
			agents: agents,
		}
	}
}

// searchCandidates returns the chats the search index should cover: only the
// workflow's chats when one is in scope, every chat otherwise.
func searchCandidates(manager *agents.AgentManager, workflow *store.Workflow) ([]store.Agent, error) {
	if workflow != nil {
		return manager.WorkflowSearchCandidates(workflow.ID)
	}

	return manager.SearchCandidates()
}

// updateSearch drives the search screen: control keys navigate and select, and
// everything else edits the live query, re-ranking the results on each change.
func (m model) updateSearch(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	k := key.String()
	switch {
	case bound(k, m.keys.Search.Quit):
		return m, tea.Quit
	case bound(k, m.keys.Search.Back):
		m.screen = screenList
		return m, nil
	case bound(k, m.keys.Nav.Up):
		if m.searchCursor > 0 {
			m.searchCursor--
		}

		return m, nil
	case bound(k, m.keys.Nav.Down):
		if m.searchCursor < len(m.searchResults)-1 {
			m.searchCursor++
		}

		return m, nil
	case bound(k, m.keys.Search.Open):
		return m.openSearchResult()
	}

	input, cmd := m.searchInput.Update(msg)
	m.searchInput = input
	m.runSearch()

	return m, cmd
}

// runSearch re-ranks the results against the current query, capping the list and
// clamping the cursor into it.
func (m *model) runSearch() {
	results := m.searchIndex.Search(m.searchInput.Value())
	if len(results) > searchResultLimit {
		results = results[:searchResultLimit]
	}

	m.searchResults = results
	if m.searchCursor >= len(results) {
		m.searchCursor = len(results) - 1
	}
	if m.searchCursor < 0 {
		m.searchCursor = 0
	}
}

// openSearchResult attaches to the chat under the cursor. The cli loop restores
// it if its pane is gone and un-deletes it if it was soft-deleted.
func (m model) openSearchResult() (tea.Model, tea.Cmd) {
	if m.searchCursor < 0 || m.searchCursor >= len(m.searchResults) {
		return m, nil
	}

	agent, ok := m.searchAgents[m.searchResults[m.searchCursor].Name]
	if !ok {
		return m, nil
	}

	m.attach = m.attachScoped(agent)
	return m, tea.Quit
}

func (m model) searchView() string {
	var out strings.Builder
	out.WriteString(headerStyle.Render("Search chats") + "\n\n")
	out.WriteString(m.searchInput.View() + "\n\n")

	switch {
	case m.searching:
		out.WriteString(dimStyle.Render("indexing…"))
	case len(m.searchResults) == 0:
		out.WriteString(dimStyle.Render("no matches"))
	default:
		out.WriteString(m.searchResultsView())
	}

	out.WriteString("\n\n" + dimStyle.Render(
		hintKeys(m.keys.Search.Open)+" open · "+
			hintKeys(m.keys.Nav.Up)+"/"+hintKeys(m.keys.Nav.Down)+" move · "+
			hintKeys(m.keys.Search.Back)+" back"))

	return out.String()
}

// searchResultsView renders the ranked matches, each a chat line plus a dim
// snippet of the transcript around the match.
func (m model) searchResultsView() string {
	var out strings.Builder
	for i, result := range m.searchResults {
		cursor := "  "
		name := result.Name
		if i == m.searchCursor {
			cursor = cursorStyle.Render("› ")
			name = cursorStyle.Render(name)
		}

		badge := ""
		if result.Deleted {
			badge = dimStyle.Render(" (deleted)")
		}

		title := ""
		if result.Title != "" {
			title = dimStyle.Render(" · " + result.Title)
		}

		out.WriteString(cursor + name + badge + title + "\n")
		if result.Snippet != "" {
			out.WriteString("    " + dimStyle.Render(truncate(result.Snippet, m.snippetWidth())) + "\n")
		}
	}

	return out.String()
}

// snippetWidth is how wide a result snippet may be before it is truncated to fit
// the popup.
func (m model) snippetWidth() int {
	width := m.width - 6
	if width < 20 {
		return 20
	}

	return width
}

// truncate shortens s to at most width columns, appending an ellipsis when cut.
func truncate(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}

	runes := []rune(s)
	if width < 1 {
		return ""
	}
	if len(runes) > width-1 {
		runes = runes[:width-1]
	}

	return string(runes) + "…"
}
