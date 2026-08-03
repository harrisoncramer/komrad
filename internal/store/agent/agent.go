// Package agent persists the agents themselves. It owns no other domain's tables, so a
// write touching an agent and its transcript is composed by a caller inside one
// transaction rather than reached for here.
package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
)

// ErrTooManyMetadataKeys reports that an agent already holds as many keys as it is
// allowed. It is a sentinel so a caller can tell a ceiling apart from a broken write.
var ErrTooManyMetadataKeys = errors.New("too many metadata keys")

// maxMetadataKeys caps how many facts one agent may record, since the keys are the
// agent's own invention and a looping agent would fill the table. It is checked here
// rather than in the proto because it needs a count of what is already stored.
const maxMetadataKeys = 32

// slotsLock is the advisory lock key the agent ceiling is claimed under. The number is
// arbitrary, but it is the whole of what makes two claimants wait for each other, so it
// must never change and nothing else may take it.
const slotsLock int64 = 7_240_116

// Agent states. There are three because there are only three things a person watching
// the panel acts on differently: an agent generating, one blocked on an answer they owe
// it, and one doing neither. An agent that has not started and one that has stopped share
// the third, since the next prompt puts either straight back to work.
const (
	StateThinking = "thinking"
	StateWaiting  = "waiting"
	StateIdle     = "idle"
)

// Metadata is one fact an agent recorded about its own work. The key is the agent's own
// invention, so nothing here interprets it.
type Metadata struct {
	Key   string
	Value string
}

// Agent is a persisted background agent.
type Agent struct {
	ID     string
	Name   string
	Pane   string
	Cwd    string
	Flags  string
	Prompt string
	State  string
	// Harness names the CLI this agent runs, which decides how it is launched, how its
	// state is read off its pane, and where its conversation log is found. It is
	// set at create and never empty.
	Harness string
	// SessionID is the conversation the agent's harness files its own log under and
	// reopens on a resume.
	SessionID string
	// WorkflowID holds the workflow's stable id, not its display name. Every agent has
	// one, since the workflow supplies the directory, flags, and context it starts from.
	WorkflowID string
	// ScheduleID is the schedule that launched this agent, nil for one a person started.
	// Provenance only.
	ScheduleID *string
	// IdempotencyKey is the caller's claim that this agent is the one that key names, so
	// a retried launch finds it rather than spawning a second one.
	IdempotencyKey *string
	// ArchivedAt is when the agent was put away, nil while it is live. It is the archive
	// itself rather than a time beside a flag, so nothing has to reconcile the two, and
	// the archive orders by it so the agent just archived is on top.
	ArchivedAt *time.Time
	// Hidden keeps this agent out of the panel's list without stopping it. It is written
	// on insert and by the user afterwards, so a reconciler pass cannot undo a hide.
	Hidden bool
	Mark   *string
	// Metadata is ordered by key, and only the listings a caller renders carry it, since
	// the reconciler walks every agent every couple of seconds and has no use for it.
	Metadata  []Metadata
	CreatedAt time.Time
	// UpdatedAt is for display. No listing orders by it, since a key that every write
	// moves reshuffles the list under the reader.
	UpdatedAt time.Time
}

// SanitizeName maps a name to the characters tmux allows in a session name,
// which is also the store key so the two always agree. Control characters go the
// same way as the punctuation, since a current tmux refuses a window name carrying
// one outright and older versions open something other than what was asked for.
func SanitizeName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r == 0x7f, r == ':', r == '.', r == ' ':
			return '-'
		default:
			return r
		}
	}, name)
}

// Store reads and writes agents.
type Store struct {
	queries *models.Queries
}

// New builds an agent store over db, either the pool or a transaction. A caller
// composing this with another domain passes the transaction, so both land together.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

// List returns every persisted agent, newest first.
func (s *Store) List(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	agents := make([]Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(*row))
	}

	return agents, nil
}

// Cursor locates an agent within a listing. The id alone is enough, since it is unique
// and never changes, so a page resumes exactly where the last one ended.
type Cursor struct {
	ID string `json:"i"`
}

// Validate refuses a cursor holding anything but an id, which is what the listing pages by.
func (c Cursor) Validate() error {
	if !store.IsID(c.ID) {
		return fmt.Errorf("cursor id %q is not an id", c.ID)
	}

	return nil
}

// ListPage returns one page of agents, newest first, resuming after cursor when one is
// given. archived picks the live agents or the archived ones and never both, since an
// archived agent may share a name with a live one. Paging keys on the id rather than
// update time, which would move an agent the reconciler touched mid-scan ahead of the
// reader and have it seen twice or missed.
func (s *Store) ListPage(ctx context.Context, limit int64, cursor *Cursor, archived bool) ([]Agent, error) {
	params := models.ListAgentsPageParams{Lim: int32(limit), Archived: archived}
	if cursor != nil {
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListAgentsPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of agents: %w", err)
	}

	agents := make([]Agent, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(*row))
		ids = append(ids, row.ID)
	}

	metadata, err := s.metadataByAgent(ctx, ids)
	if err != nil {
		return nil, err
	}

	for i := range agents {
		agents[i].Metadata = metadata[agents[i].ID]
	}

	return agents, nil
}

// metadataByAgent reads the metadata of a whole page of agents in one query, keyed by the
// agent it belongs to. It is kept out of agentFromRow because the reconciler walks every
// agent on a two second timer and would pay for a query it never reads.
func (s *Store) metadataByAgent(ctx context.Context, ids []string) (map[string][]Metadata, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListAgentMetadata(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to list agent metadata: %w", err)
	}

	byAgent := make(map[string][]Metadata, len(ids))
	for _, row := range rows {
		byAgent[row.AgentID] = append(byAgent[row.AgentID], Metadata{Key: row.Key, Value: row.Value})
	}

	return byAgent, nil
}

// ListAll returns every agent including archived ones, newest first, for search.
func (s *Store) ListAll(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListAllAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list all agents: %w", err)
	}

	agents := make([]Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(*row))
	}

	return agents, nil
}

// GetInWorkflow returns the live agent a workflow holds under a name, nil when it holds
// none. A name is unique inside a workflow and nowhere wider, so this is the narrowest
// lookup a name answers.
func (s *Store) GetInWorkflow(ctx context.Context, workflowID, name string) (*Agent, error) {
	row, err := s.queries.GetAgentInWorkflow(ctx, models.GetAgentInWorkflowParams{
		WorkflowID: workflowID,
		Name:       name,
	})
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q in workflow %q: %w", name, workflowID, err)
	}

	metadata, err := s.metadataByAgent(ctx, []string{row.ID})
	if err != nil {
		return nil, err
	}

	agent := agentFromRow(*row)
	agent.Metadata = metadata[agent.ID]

	return &agent, nil
}

// GetByID returns an agent by id, nil when none holds it. It spans the archived agents,
// since an id is the only handle one of those still answers to: a live agent may have
// taken its name since.
func (s *Store) GetByID(ctx context.Context, id string) (*Agent, error) {
	if !store.IsID(id) {
		return nil, nil
	}

	row, err := s.queries.GetAgentByID(ctx, id)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", id, err)
	}

	metadata, err := s.metadataByAgent(ctx, []string{row.ID})
	if err != nil {
		return nil, err
	}

	agent := agentFromRow(*row)
	agent.Metadata = metadata[agent.ID]

	return &agent, nil
}

// ListByID returns the agents holding the given ids, newest first, skipping any id no
// agent holds. It spans the archived agents for the reason GetByID does, and is what a
// call keyed on a list of ids reads before it writes.
func (s *Store) ListByID(ctx context.Context, ids []string) ([]Agent, error) {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.GetAgentsByID(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to get %d agents: %w", len(storable), err)
	}

	agents := make([]Agent, 0, len(rows))
	found := make([]string, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(*row))
		found = append(found, row.ID)
	}

	metadata, err := s.metadataByAgent(ctx, found)
	if err != nil {
		return nil, err
	}

	for i := range agents {
		agents[i].Metadata = metadata[agents[i].ID]
	}

	return agents, nil
}

// Exists reports whether any agent holds the given id, including archived ones.
func (s *Store) Exists(ctx context.Context, id string) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	exists, err := s.queries.AgentExists(ctx, id)
	if err != nil {
		return false, fmt.Errorf("failed to check for agent %q: %w", id, err)
	}

	return exists, nil
}

// ActiveNameExists reports whether a live agent in the workflow already holds the name,
// including a dormant one whose pane is gone but whose record survives.
func (s *Store) ActiveNameExists(ctx context.Context, workflowID, name string) (bool, error) {
	exists, err := s.queries.ActiveNameExists(ctx, models.ActiveNameExistsParams{
		WorkflowID: workflowID,
		Name:       name,
	})
	if err != nil {
		return false, fmt.Errorf("failed to check active name %q in workflow %q: %w", name, workflowID, err)
	}

	return exists, nil
}

// Create inserts a fresh agent under the id it carries. The id is the caller's rather
// than the database's because it names the agent everywhere outside this table: the pane
// is stamped with it, and the agent is told it at launch. A caller with no id yet gets one
// minted here, which is every test and nothing in production.
func (s *Store) Create(ctx context.Context, agent Agent) error {
	if agent.ID == "" {
		id, err := store.NewID()
		if err != nil {
			return err
		}

		agent.ID = id
	}

	if _, err := s.queries.InsertAgent(ctx, models.InsertAgentParams{
		ID:             agent.ID,
		Name:           agent.Name,
		Cwd:            agent.Cwd,
		Flags:          agent.Flags,
		Prompt:         agent.Prompt,
		PaneID:         agent.Pane,
		State:          agent.State,
		Harness:        agent.Harness,
		SessionID:      agent.SessionID,
		WorkflowID:     agent.WorkflowID,
		ScheduleID:     agent.ScheduleID,
		IdempotencyKey: agent.IdempotencyKey,
		IsHidden:       agent.Hidden,
	}); err != nil {
		return fmt.Errorf("failed to insert agent %q: %w", agent.Name, err)
	}

	return nil
}

// Update rewrites an existing agent's editable and runtime fields, keyed by its
// stable id.
func (s *Store) Update(ctx context.Context, agent Agent) error {
	if err := s.queries.UpdateAgent(ctx, models.UpdateAgentParams{
		ID:         agent.ID,
		Cwd:        agent.Cwd,
		Flags:      agent.Flags,
		Prompt:     agent.Prompt,
		PaneID:     agent.Pane,
		State:      agent.State,
		SessionID:  agent.SessionID,
		WorkflowID: agent.WorkflowID,
	}); err != nil {
		return fmt.Errorf("failed to update agent %q: %w", agent.Name, err)
	}

	return nil
}

// SetStateByID updates just an agent's state, keyed by id so a write the reconciler
// decided from a snapshot lands on the row it observed even if that name was reused.
func (s *Store) SetStateByID(ctx context.Context, id, state string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.SetAgentStateByID(ctx, models.SetAgentStateByIDParams{ID: id, State: state}); err != nil {
		return fmt.Errorf("failed to set state for id %q: %w", id, err)
	}

	return nil
}

// SetSessionID records the conversation an agent turned out to be holding, for a harness
// that names its own. It writes only while the agent still has none, so a second pass
// cannot move an agent onto another conversation.
func (s *Store) SetSessionID(ctx context.Context, id, sessionID string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.SetAgentSessionID(ctx, models.SetAgentSessionIDParams{ID: id, SessionID: sessionID}); err != nil {
		return fmt.Errorf("failed to set the session for id %q: %w", id, err)
	}

	return nil
}

// ClearPane drops the pane an agent claims to be running in. A pane id is only unique
// while the tmux server that issued it lives, so a dormant agent holding one would go on
// claiming an id the next server hands to somebody else.
func (s *Store) ClearPane(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.ClearAgentPane(ctx, id); err != nil {
		return fmt.Errorf("failed to clear pane for id %q: %w", id, err)
	}

	return nil
}

// SetMarks puts one mark on every agent named, keyed by id, and a nil mark clears the
// ones they carry. It reports how many rows were touched, so a call that reached
// nothing is distinguishable from one that reached some of what it named.
func (s *Store) SetMarks(ctx context.Context, ids []string, mark *string) (int, error) {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return 0, nil
	}

	updated, err := s.queries.SetAgentMarks(ctx, models.SetAgentMarksParams{Ids: storable, Mark: mark})
	if err != nil {
		return 0, fmt.Errorf("failed to mark %d agents: %w", len(storable), err)
	}

	return len(updated), nil
}

// SetHidden hides an agent from the panel's list or brings it back, keyed by id.
// An archived agent has no listing to be hidden from, so it is not a row this
// writes.
func (s *Store) SetHidden(ctx context.Context, id string, hidden bool) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	updated, err := s.queries.SetAgentHidden(ctx, models.SetAgentHiddenParams{ID: id, Hidden: hidden})
	if err != nil {
		return false, fmt.Errorf("failed to hide agent %q: %w", id, err)
	}

	return len(updated) > 0, nil
}

// SetMetadataParams identifies the fact being recorded. A nil Value clears the key
// rather than storing an empty one.
type SetMetadataParams struct {
	ID    string
	Key   string
	Value *string
}

// SetMetadata records one fact an agent found out about its own work, replacing
// whatever that key held and leaving its other keys untouched. It reports whether a
// row was written.
func (s *Store) SetMetadata(ctx context.Context, params SetMetadataParams) (bool, error) {
	if !store.IsID(params.ID) {
		return false, nil
	}

	if params.Value == nil {
		deleted, err := s.queries.DeleteAgentMetadata(ctx, models.DeleteAgentMetadataParams{AgentID: params.ID, Key: params.Key})
		if err != nil {
			return false, fmt.Errorf("failed to clear metadata %q on agent %q: %w", params.Key, params.ID, err)
		}

		return len(deleted) > 0, nil
	}

	// The ceiling is checked before the write, and a key the agent already holds is a
	// revision that passes however full it is.
	held, err := s.queries.ListAgentMetadata(ctx, []string{params.ID})
	if err != nil {
		return false, fmt.Errorf("failed to read metadata on agent %q: %w", params.ID, err)
	}

	revising := slices.ContainsFunc(held, func(row *models.ListAgentMetadataRow) bool {
		return row.Key == params.Key
	})
	if len(held) >= maxMetadataKeys && !revising {
		return false, ErrTooManyMetadataKeys
	}

	written, err := s.queries.SetAgentMetadata(ctx, models.SetAgentMetadataParams{
		AgentID: params.ID,
		Key:     params.Key,
		Value:   *params.Value,
	})
	if err != nil {
		return false, fmt.Errorf("failed to record metadata %q on agent %q: %w", params.Key, params.ID, err)
	}

	return len(written) > 0, nil
}

// GetByIdempotencyKey returns the agent recorded under a caller's key, nil when none is.
// It spans archived agents, since one archived since the first call is still that agent
// and returning nil would spawn a duplicate.
func (s *Store) GetByIdempotencyKey(ctx context.Context, key string) (*Agent, error) {
	if key == "" {
		return nil, nil
	}

	row, err := s.queries.GetAgentByIdempotencyKey(ctx, &key)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the agent for idempotency key %q: %w", key, err)
	}

	agent := agentFromRow(*row)

	return &agent, nil
}

// CountLive reports how many agents the panel still holds, archived ones aside, a dormant
// one counting since it is still a row somebody has to archive.
func (s *Store) CountLive(ctx context.Context) (int64, error) {
	count, err := s.queries.CountLiveAgents(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to count the live agents: %w", err)
	}

	return count, nil
}

// LockSlots holds the live count still until the caller's transaction ends, so counting
// the agents and writing under that count act as one. Every caller claiming room under
// the ceiling takes it. Outside a transaction it guards nothing, since the statement that
// takes the lock also releases it.
func (s *Store) LockSlots(ctx context.Context) error {
	if err := s.queries.LockAgentSlots(ctx, slotsLock); err != nil {
		return fmt.Errorf("failed to lock the agent slots: %w", err)
	}

	return nil
}

// CountLiveForSchedule reports how many of a schedule's agents are still working, which
// is what tells a firing that the last run has not finished. One waiting on a
// confirmation counts; an idle one does not, since idle is also what a launch that died
// as it was typed keeps for good, and counting it would block the schedule forever.
func (s *Store) CountLiveForSchedule(ctx context.Context, scheduleID string) (int64, error) {
	if !store.IsID(scheduleID) {
		return 0, nil
	}

	count, err := s.queries.CountLiveScheduleAgents(ctx, &scheduleID)
	if err != nil {
		return 0, fmt.Errorf("failed to count the live agents of schedule %q: %w", scheduleID, err)
	}

	return count, nil
}

// Delete removes agents, keyed by id so it reaches the archived rows too. The
// transcripts keyed to the same ids are the transcript package's to remove, in the same
// transaction.
func (s *Store) Delete(ctx context.Context, ids []string) error {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.DeleteAgentsByID(ctx, storable); err != nil {
		return fmt.Errorf("failed to delete %d agents: %w", len(storable), err)
	}

	return nil
}

// Rename changes an agent's display name, keyed by id, reporting whether a row was
// renamed.
func (s *Store) Rename(ctx context.Context, id, name string) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	renamed, err := s.queries.RenameAgent(ctx, models.RenameAgentParams{ID: id, Name: name})
	if db.IsUniqueViolation(err) {
		return false, store.ErrNameTaken
	}
	if err != nil {
		return false, fmt.Errorf("failed to rename agent %q: %w", id, err)
	}

	return len(renamed) > 0, nil
}

// Archive hides agents from listings without dropping their rows, keyed by id. The panes
// go with them, since the reconciler that would clear a stale id skips archived rows.
func (s *Store) Archive(ctx context.Context, ids []string) error {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.MarkAgentsArchived(ctx, storable); err != nil {
		return fmt.Errorf("failed to archive %d agents: %w", len(storable), err)
	}

	return nil
}

// Restore clears the archive time of agents, keyed by id because an archived row can share
// a name with a live agent. The hidden flag goes with it, since an agent that came back
// still hidden reads as a restore that did nothing.
func (s *Store) Restore(ctx context.Context, ids []string) error {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.RestoreAgents(ctx, storable); err != nil {
		return fmt.Errorf("failed to restore %d agents: %w", len(storable), err)
	}

	return nil
}

// ArchiveWorkflowAgents archives every live agent in a workflow, keeping their
// workflow id so they still list under it in search.
func (s *Store) ArchiveWorkflowAgents(ctx context.Context, workflowID string) error {
	if err := s.queries.MarkWorkflowAgentsArchived(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to archive agents of workflow %q: %w", workflowID, err)
	}

	return nil
}

// RestoreWorkflowAgents brings every archived agent in a workflow back as a row. They
// come back dormant, so their conversations are resumed one at a time.
func (s *Store) RestoreWorkflowAgents(ctx context.Context, workflowID string) error {
	if err := s.queries.MarkWorkflowAgentsRestored(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to restore agents of workflow %q: %w", workflowID, err)
	}

	return nil
}

// ListLiveInWorkflow is the unarchived agents belonging to one workflow, whose panes a
// workflow archive tears down before it writes.
func (s *Store) ListLiveInWorkflow(ctx context.Context, workflowID string) ([]Agent, error) {
	rows, err := s.queries.ListLiveWorkflowAgents(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list live agents of workflow %q: %w", workflowID, err)
	}

	agents := make([]Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(*row))
	}

	return agents, nil
}

// ListArchivedInWorkflow is the archived agents belonging to one workflow, which is what
// a workflow restore has to make room and free names for before it writes.
func (s *Store) ListArchivedInWorkflow(ctx context.Context, workflowID string) ([]Agent, error) {
	rows, err := s.queries.ListArchivedWorkflowAgents(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list archived agents of workflow %q: %w", workflowID, err)
	}

	agents := make([]Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(*row))
	}

	return agents, nil
}

// keepStorableIDs drops the ids no row could hold. Postgres refuses a whole uuid array
// carrying one member it cannot parse, so an id the caller made up would otherwise take
// the agents alongside it down with it.
func keepStorableIDs(ids []string) []string {
	storable := make([]string, 0, len(ids))
	for _, id := range ids {
		if store.IsID(id) {
			storable = append(storable, id)
		}
	}

	return storable
}

func agentFromRow(agent models.Agent) Agent {
	return Agent{
		ID:             agent.ID,
		Name:           agent.Name,
		Pane:           agent.PaneID,
		Cwd:            agent.Cwd,
		Flags:          agent.Flags,
		Prompt:         agent.Prompt,
		State:          agent.State,
		Harness:        agent.Harness,
		SessionID:      agent.SessionID,
		WorkflowID:     agent.WorkflowID,
		ScheduleID:     agent.ScheduleID,
		IdempotencyKey: agent.IdempotencyKey,
		ArchivedAt:     agent.ArchivedAt,
		Hidden:         agent.IsHidden,
		Mark:           agent.Mark,
		CreatedAt:      agent.CreatedAt,
		UpdatedAt:      agent.UpdatedAt,
	}
}
