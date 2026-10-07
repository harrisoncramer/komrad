package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"komrad/config"
	"komrad/internal/client"
	"komrad/internal/errui"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/paint"
	"komrad/internal/view"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	paginationv1 "komrad/proto/gen/pagination/v1"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
)

// searchScreen is the search screen: the query being typed, the hits the server found for it,
// the cursor in them, and the transcript drawn beside them.
type searchScreen struct {
	rowCursor
	typing bool
	input  lineInput
	// turns are the hits the server found in the conversations, one per turn that said the query.
	turns []searchHit
	// drawn is the rows built for the frame once an update settles, and nil while one runs.
	drawn   []searchRow
	next    *string
	paging  bool
	problem searchProblem
	// preview is the transcript of the agent the cursor is standing on, drawn beside the list.
	preview        searchPreview
	previewFocused bool
}

func (w *searchScreen) list() screenList[searchRow] {
	return screenList[searchRow]{
		cursor: func() *rowCursor { return &w.rowCursor },
		rows:   w.rows,
		moved:  func(m *model) tea.Cmd { return m.waitToStage() },
	}
}

func (w *searchScreen) landOnRow(m *model) tea.Cmd {
	row, ok := w.hovered(m)
	if !ok {
		return nil
	}

	var cmds []tea.Cmd
	if w.preview.agentID == row.agent.id && w.preview.conversation != "" {
		w.openPreviewOn(m, row.open(w.preview.conversation))
	} else {
		cmds = append(cmds, w.previewCmd(m, row.agent))
	}

	if w.cursor == len(w.rows(m))-1 && w.next != nil && !w.paging {
		w.paging = true
		cmds = append(cmds, w.searchCmd(m, w.query(), w.next))
	}

	return tea.Batch(cmds...)
}

// actions is the keys of whichever of the list and the transcript holds them.
func (w *searchScreen) actions(m *model) screenActionSpec {
	if w.isBlockFocused(m) {
		return w.previewActions()
	}

	return w.listActions()
}

// listActions is the search screen as one table once the query line has given the keys up.
func (w *searchScreen) listActions() screenActionSpec {
	list := w.list()
	shared := slices.DeleteFunc(list.actions(), func(a screenAction) bool { return a.id == actionColumnScroll })

	return screenActionSpec{
		table: append(shared,
			screenAction{
				id:   actionColumnScroll,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Column.ScrollDown, k.Column.ScrollUp) },
				nav:  func(m *model, pressed string) tea.Cmd { return w.scrollPreview(m, pressed) },
			},
			screenAction{
				id:   actionSearchEdit,
				keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				nav: func(*model, string) tea.Cmd {
					w.typing = true
					w.input.insert()

					return nil
				},
			},
			screenAction{
				id:   actionSearchBack,
				keys: fixedBack,
				nav:  func(m *model, _ string) tea.Cmd { return m.pickView() },
			},
			screenAction{
				id:   actionSearchOpen,
				keys: func(config.Keybindings) []string { return []string{"enter"} },
				nav:  func(m *model, _ string) tea.Cmd { return w.jumpToAgent(m) },
			},
		),
		legend: slices.Concat([]screenLegendRow{
			{
				of:    []actionID{actionSearchOpen},
				desc:  func(*model) string { return "go to it" },
				strip: true,
			},
			{
				of:    []actionID{actionSearchEdit},
				desc:  func(*model) string { return "edit the query" },
				strip: true,
			},
		}, screenNavLegend(), []screenLegendRow{
			{
				of:   []actionID{actionSearchBack},
				desc: func(*model) string { return "back" },
			},
		}),
	}
}

const searchLimit = 10

const searchDebounce = 120 * time.Millisecond

type searchHit struct {
	agentID  string
	name     string
	archived bool
	seq      int32
	role     string
	offset   int64
	length   int64
	snippet  string
}

func (h searchHit) lead() string {
	speaker := speakerOf(h.role)
	if speaker == "" {
		return fmt.Sprintf("#%d", h.seq)
	}

	return fmt.Sprintf("%s #%d", speaker, h.seq)
}

type searchRow struct {
	agent searchAgent
	hit   *searchHit
}

func (r searchRow) key() string {
	if r.hit == nil {
		return r.agent.id
	}

	return fmt.Sprintf("%s/%d", r.agent.id, r.hit.seq)
}

func (r searchRow) open(content string) int {
	if r.hit == nil {
		return 0
	}

	own := r.hit.spans(content)
	if len(own) == 0 {
		return 0
	}

	return max(slices.IndexFunc(r.agent.spans(content), func(span paint.Span) bool {
		return span.Start == own[0].Start
	}), 0)
}

// searchAgent is one row of the search: an agent, with the turns of its conversation that said
// the query.
type searchAgent struct {
	id       string
	name     string
	archived bool
	turns    []searchHit
}

func (a searchAgent) key() string {
	return a.id
}

type searchSettledMsg struct {
	query string
}

type searchResult struct {
	query   string
	hits    []searchHit
	next    *string
	paged   bool
	problem searchProblem
}

type searchProblem struct {
	reason  string
	refused bool
}

type previewResult struct {
	agentID string
	query   string
	content string
}

// enter opens the search fresh, with the query line waiting for the filter key.
func (w *searchScreen) enter(m *model, target screenTarget) tea.Cmd {
	if target.resume {
		return nil
	}

	*w = searchScreen{}

	return nil
}

func debounceSearchCmd(query string) tea.Cmd {
	if strings.TrimSpace(query) == "" {
		return func() tea.Msg { return searchResult{query: query} }
	}

	return startTimer(searchDebounce, func(time.Time) tea.Msg {
		return searchSettledMsg{query: query}
	})
}

func (w *searchScreen) runSettled(m *model, msg searchSettledMsg) tea.Cmd {
	if msg.query != w.query() {
		return nil
	}

	return w.searchCmd(m, msg.query, nil)
}

func (w *searchScreen) searchCmd(m *model, query string, cursor *string) tea.Cmd {
	client := m.api.reads().agents

	return func() tea.Msg {
		if strings.TrimSpace(query) == "" {
			return searchResult{query: query}
		}

		page, err := call(agentsTimeout, func(ctx context.Context) (searchResult, error) {
			resp, err := client.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
				Query: query,
				Pagination: &paginationv1.Pagination{
					Limit:  xptr.Ptr[int32](searchLimit),
					Cursor: cursor,
				},
			}))
			if err != nil {
				return searchResult{}, err
			}

			// The turns that say the words stand before the ones that only carry the letters.
			found := slices.Concat(resp.Msg.GetMatches(), resp.Msg.GetNear())

			return searchResult{
				query: query,
				hits:  xslice.Map(found, searchHitFrom),
				next:  resp.Msg.NextPageToken,
				paged: cursor != nil,
			}, nil
		})
		if err != nil {
			return searchResult{query: query, paged: cursor != nil, problem: searchProblem{
				reason:  errui.GetReason(err),
				refused: connect.CodeOf(err) == connect.CodeInvalidArgument,
			}}
		}

		return page
	}
}

func searchHitFrom(h *agentv1.SearchHit) searchHit {
	return searchHit{
		agentID:  h.GetAgentId(),
		name:     h.GetName(),
		archived: h.GetArchived(),
		seq:      h.GetSeq(),
		role:     h.GetRole(),
		offset:   h.GetContentOffset(),
		length:   h.GetContentLength(),
		snippet:  h.GetSnippet(),
	}
}

// place puts the cursor back on the row it was standing on, and fetches a preview where that
// row has gone.
func (w *searchScreen) place(m *model, was string) tea.Cmd {
	rows := w.rows(m)
	if len(rows) == 0 {
		w.cursor = 0
		w.preview = searchPreview{}

		return nil
	}

	at := rowIndexOfKey(rows, was)
	w.cursor = cursor.Place(at, w.cursor, len(rows))
	if at >= 0 {
		return nil
	}

	return w.previewCmd(m, rows[w.cursor].agent)
}

// rows is every agent the search found, once each, the named ones first, with the turns of its
// conversation that said the query under it.
func (w *searchScreen) rows(m *model) []searchRow {
	if w.drawn != nil {
		return w.drawn
	}

	return w.buildRows(m)
}

func (w *searchScreen) buildFrame(m *model) {
	w.drawn = w.buildRows(m)
}

func (w *searchScreen) buildRows(m *model) []searchRow {
	var rows []searchRow
	for _, agent := range w.buildAgents(m) {
		rows = append(rows, searchRow{agent: agent})
		for i := range agent.turns {
			rows = append(rows, searchRow{agent: agent, hit: &agent.turns[i]})
		}
	}

	return rows
}

func (w *searchScreen) buildAgents(m *model) []searchAgent {
	query := strings.TrimSpace(w.query())
	if query == "" {
		return nil
	}

	var agents []searchAgent
	at := make(map[string]int)
	add := func(agent searchAgent) int {
		if i, ok := at[agent.id]; ok {
			return i
		}
		at[agent.id] = len(agents)
		agents = append(agents, agent)

		return len(agents) - 1
	}

	for _, item := range m.listing.agents {
		if doesNameSay(item.Agent.Name, query) {
			add(searchAgent{id: item.Agent.ID, name: item.Agent.Name})
		}
	}
	for _, item := range m.listing.archivedAgents {
		if doesNameSay(item.Agent.Name, query) {
			add(searchAgent{
				id:       item.Agent.ID,
				name:     item.Agent.Name,
				archived: true,
			})
		}
	}
	for _, hit := range w.turns {
		i := add(searchAgent{
			id:       hit.agentID,
			name:     hit.name,
			archived: hit.archived,
		})
		agents[i].turns = append(agents[i].turns, hit)
	}

	return agents
}

func doesNameSay(name, query string) bool {
	return strings.Contains(paint.FoldASCII(name), paint.FoldASCII(query))
}

// nameMatches is where in a name the query was found, in bytes of that name.
func nameMatches(name, query string) []view.Span {
	folded := paint.FoldASCII(strings.TrimSpace(query))
	if folded == "" {
		return nil
	}

	at := strings.Index(paint.FoldASCII(name), folded)
	if at < 0 {
		return nil
	}

	return []view.Span{{Start: at, End: at + len(folded)}}
}

func (w *searchScreen) apply(m *model, result searchResult) tea.Cmd {
	if result.query != w.query() {
		return nil
	}

	w.problem = result.problem
	if result.problem.reason != "" {
		w.paging = false

		return nil
	}

	if result.paged {
		was := w.list().hovered(m)
		w.paging = false
		w.turns = append(w.turns, result.hits...)
		w.next = result.next

		rows := w.rows(m)
		w.cursor = cursor.Place(rowIndexOfKey(rows, was), w.cursor, len(rows))

		return w.landOnRow(m)
	}

	w.turns = result.hits
	w.next = result.next
	w.paging = false

	rows := w.rows(m)
	w.cursor = cursor.Clamp(w.cursor, len(rows))
	if len(rows) == 0 {
		w.preview = searchPreview{}

		return nil
	}

	return w.previewCmd(m, rows[w.cursor].agent)
}

func (w *searchScreen) previewCmd(m *model, agent searchAgent) tea.Cmd {
	agents := m.api.reads().agents
	query := w.query()

	return func() tea.Msg {
		content, err := call(agentsTimeout, func(ctx context.Context) (string, error) {
			transcript, err := client.GetFullTranscript(ctx, agents, agent.id)
			if err != nil {
				return "", err
			}

			return paint.ScrubMarks(transcript), nil
		})
		if err != nil {
			return previewResult{agentID: agent.id, query: query}
		}

		return previewResult{
			agentID: agent.id,
			query:   query,
			content: content,
		}
	}
}

func (w *searchScreen) applyPreview(m *model, result previewResult) {
	if result.query != w.query() {
		return
	}

	row, ok := w.hovered(m)
	if !ok || row.agent.id != result.agentID {
		return
	}

	w.holdPreview(m, holdPreviewParams{
		agentID:      row.agent.id,
		conversation: result.content,
		spans:        row.agent.spans(result.content),
		open:         row.open(result.content),
	})
}

const noTranscriptMarkdown = "_no transcript yet_"

// The three things the column beside the list says before it has a transcript to draw.
const (
	searchWaitingMarkdown = "_the conversation the cursor is standing on is read here_"
	noSearchHitsMarkdown  = "_nothing said that_"
	searchLoadingMarkdown = "_reading the conversation…_"
)

// update takes the search's own results and, once the query line has given them up, the keys.
func (w *searchScreen) update(m *model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case searchSettledMsg:
		return w.runSettled(m, msg)
	case searchResult:
		return w.apply(m, msg)
	case previewResult:
		w.applyPreview(m, msg)

		return nil
	case tea.PasteMsg:
		if !w.typing {
			return nil
		}

		w.input.take(msg, m.register)

		return debounceSearchCmd(w.query())
	case tea.KeyPressMsg:
		if w.typing {
			return w.updateInput(m, msg)
		}

		if w.isBlockFocused(m) {
			return w.previewActions().dispatchTop(m, msg.String())
		}

		return w.list().update(m, msg, w.listActions())
	}

	return nil
}

func (w *searchScreen) updateInput(m *model, key tea.KeyPressMsg) tea.Cmd {
	passes := slices.Concat(m.keys.Nav.PageUp, m.keys.Nav.PageDown, m.keys.Column.ScrollDown, m.keys.Column.ScrollUp)
	if typedText(key) == "" && isBound(key.String(), passes) {
		return w.listActions().dispatch(m, key.String())
	}

	if key.String() == "ctrl+c" {
		return m.hide()
	}

	switch w.input.take(key, m.register) {
	case lineLeft, lineEntered:
		w.typing = false
	case lineChanged:
		return debounceSearchCmd(w.query())
	case lineIgnored, lineKept:
	}

	return nil
}

func (w *searchScreen) hovered(m *model) (searchRow, bool) {
	return rowAt(w.rows(m), w.cursor)
}

func (w *searchScreen) hoveredName(m *model) string {
	if w.typing {
		return ""
	}

	agent, ok := w.hoveredAgent(m)
	if !ok {
		return ""
	}

	return agent.name
}

func (w *searchScreen) hoveredAgent(m *model) (searchAgent, bool) {
	row, ok := w.hovered(m)

	return row.agent, ok
}

// jumpToAgent lands the agents list or the archive on the hovered row's agent.
func (w *searchScreen) jumpToAgent(m *model) tea.Cmd {
	agent, ok := w.hoveredAgent(m)
	if !ok {
		return nil
	}

	*w = searchScreen{}

	target := screenTarget{agentID: agent.id}
	if agent.archived {
		return m.enterArchived(target)
	}

	return m.enterLive(target)
}

// help is the search's legend: the query line's two keys while it is typed at, and the list's
// own once it has given the keys up.
func (w *searchScreen) help(m *model) keyMap {
	if w.typing {
		done := binding([]string{"enter"}, "keep it, then move through the hits")
		leave := binding([]string{"esc"}, "leave the query line")

		return keyMap{short: []key.Binding{done, leave}, full: []key.Binding{done, leave}}
	}

	own := w.actions(m).help(m)

	return keyMap{
		short: slices.Concat(own.short, m.globalHelp()),
		full:  slices.Concat(own.full, m.fullGlobalHelp()),
	}
}

var (
	_ columnFocuser = (*searchScreen)(nil)
	_ hoveredNamer  = (*searchScreen)(nil)
	_ lineTyper     = (*searchScreen)(nil)
	_ listScroller  = (*searchScreen)(nil)
)

func (w *searchScreen) doesDrawFromListing() bool {
	return false
}

func (w *searchScreen) takesEveryKey() bool {
	return w.typing
}

func (w *searchScreen) column() columnSpec {
	return searchColumn()
}

func speakerOf(role string) string {
	switch role {
	case "user":
		return "you"
	case "assistant":
		return "agent"
	}

	return role
}

func (w *searchScreen) query() string {
	return w.input.value()
}

func (w *searchScreen) typedLine() (lineInput, bool) {
	return w.input, w.typing
}

func (w *searchScreen) dispatches(*model) []dispatching {
	return []dispatching{w.listActions(), w.previewActions()}
}
