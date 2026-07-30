// Package agent persists the agents themselves, the agents table and the
// per-agent config row that hangs off it.
//
// It owns no other domain's tables. An agent's transcript belongs to the
// transcript package and the workflow an agent sits in belongs to the workflow
// package, so an operation touching both is composed by a caller inside one
// transaction rather than reached for from here.
package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"claude-agents/db/models"
	"claude-agents/internal/store"

	"github.com/jackc/pgx/v5/pgconn"
)

// Agent lifecycle states.
const (
	StateNew      = "new"
	StateThinking = "thinking"
	StateWaiting  = "waiting"
	StateDone     = "done"
)

// Metadata is one fact an agent recorded about its own work, such as the issue it
// is implementing or the pull request it opened. The key is the agent's own
// invention rather than one of a fixed set, so nothing here interprets it.
type Metadata struct {
	Key   string
	Value string
}

// Agent is a persisted background agent.
type Agent struct {
	ID   string
	Name string
	Pane string
	// Cwd is where the agent runs and Flags are what claude was launched with. A
	// restore reads both back to bring it up the same way.
	Cwd             string
	Flags           string
	Prompt          string
	State           string
	ClaudeSessionID string
	// WorkflowID is the workflow this agent belongs to, and every agent has one,
	// since the workflow is where the directory, flags, and shared context it starts
	// from come from. It holds the workflow's stable id, not its display name.
	WorkflowID string
	// ScheduleID is the schedule that launched this agent, or nil for one a person
	// started. It is provenance only, and nothing about the agent depends on it.
	ScheduleID *string
	// IdempotencyKey is the caller's claim that this agent is the one identified by
	// that key, nil for a create that makes no such claim. A retried launch passing
	// the same key finds this agent rather than spawning a second one.
	IdempotencyKey *string
	Deleted        bool
	// Hidden keeps this agent out of the panel's list without stopping it. It runs and
	// is reconciled the same whichever way it is set. It is written on insert, from the
	// launching schedule's HideAgents, and by the user afterwards. Nothing else touches
	// it, so an update never carries it and a reconciler pass cannot undo a hide.
	Hidden bool
	// Mark is the glyph the user put on this agent, nil when it carries none. Only
	// the user sets and clears it, so it says nothing about the agent's lifecycle.
	Mark *string
	// Metadata is what the agent recorded about its own work, ordered by key. Only
	// the listings a caller renders carry it, since the reconciler reads every agent
	// every couple of seconds and has no use for it.
	Metadata []Metadata
	// CreatedAt is when the agent was first recorded, for display.
	CreatedAt time.Time
	// UpdatedAt is when the row last changed, for display. No listing orders by it,
	// since a key that every write moves reshuffles the list under the reader.
	UpdatedAt time.Time
}

// SanitizeName maps a name to the characters tmux allows in a session name,
// which is also the store key so the two always agree.
func SanitizeName(name string) string {
	return strings.NewReplacer(":", "-", ".", "-", " ", "-").Replace(name)
}

// Store reads and writes agents.
type Store struct {
	queries *models.Queries
}

// New builds an agent store over db, which is either the pool or a transaction. A
// caller composing this with another domain passes the transaction, so both land
// together.
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

// Cursor locates an agent within a listing. The id alone is enough, since it is
// unique and never changes, so the order it gives is total and a page resumes
// exactly where the last one ended.
type Cursor struct {
	ID string `json:"i"`
}

// ListPage returns one page of agents, newest first, resuming after cursor when
// one is given. deleted picks which set is listed, the live agents or the
// soft-deleted ones, and never both: the trash is its own view, and a deleted
// agent may share a name with a live one. The key is the id rather than anything
// the agent can change. A uuidv7 is assigned once and sorts by the moment it was
// assigned, so the newest agent goes to the top and stays there. Keying on update
// time would move an agent the reconciler touched mid-scan ahead of where the
// caller already is, and it would be seen twice or missed.
func (s *Store) ListPage(ctx context.Context, limit int64, cursor *Cursor, deleted bool) ([]Agent, error) {
	params := models.ListAgentsPageParams{Lim: int32(limit), Deleted: deleted}
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

// metadataByAgent reads the metadata of a whole page of agents in one query rather
// than one per row, keyed by the agent it belongs to. Only the listings something
// renders read it, which is why it is not folded into agentFromRow: the reconciler
// walks every agent on a two second timer and would pay for a second query it never
// reads.
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

// ListAll returns every agent including soft-deleted ones, newest first. It
// backs search, which spans active and deleted agents.
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

// Get returns an agent by name, nil when none exists, or an error for a real
// failure. No-rows is the nil case. Every other error is surfaced so a caller does
// not report a genuine DB failure as not found.
func (s *Store) Get(ctx context.Context, name string) (*Agent, error) {
	row, err := s.queries.GetAgent(ctx, name)
	if store.NoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", name, err)
	}

	metadata, err := s.metadataByAgent(ctx, []string{row.ID})
	if err != nil {
		return nil, err
	}

	agent := agentFromRow(*row)
	agent.Metadata = metadata[agent.ID]

	return &agent, nil
}

// Exists reports whether any agent holds the given id, including soft-deleted
// ones, so a caller can tell an unknown id from an agent that has nothing stored
// yet.
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

// ActiveNameExists reports whether a live, non-deleted agent already holds the
// name. New agents consult this so an auto-generated name never collides with an
// agent whose row still owns it, including a dormant one whose tmux session is
// gone but whose record survives.
func (s *Store) ActiveNameExists(ctx context.Context, name string) (bool, error) {
	exists, err := s.queries.ActiveNameExists(ctx, name)
	if err != nil {
		return false, fmt.Errorf("failed to check active name %q: %w", name, err)
	}

	return exists, nil
}

// Create inserts a fresh agent under the id it carries. The id is the caller's rather than the database's because it is the
// name of the agent everywhere outside this table: the pane it runs in is stamped
// with it, and the agent itself is told it at launch. A caller with no id yet gets
// one minted here, which is every test and nothing in production.
//
// Both halves must land together, so a caller outside a transaction gets one
// through InTx. Inside one, this joins whatever is already open.
func (s *Store) Create(ctx context.Context, agent Agent) error {
	if agent.ID == "" {
		id, err := store.NewID()
		if err != nil {
			return err
		}

		agent.ID = id
	}

	if _, err := s.queries.InsertAgent(ctx, models.InsertAgentParams{
		ID:              agent.ID,
		Name:            agent.Name,
		Cwd:             agent.Cwd,
		Flags:           agent.Flags,
		Prompt:          agent.Prompt,
		PaneID:          agent.Pane,
		State:           agent.State,
		ClaudeSessionID: agent.ClaudeSessionID,
		WorkflowID:      agent.WorkflowID,
		ScheduleID:      agent.ScheduleID,
		IdempotencyKey:  agent.IdempotencyKey,
		IsHidden:        agent.Hidden,
	}); err != nil {
		return fmt.Errorf("failed to insert agent %q: %w", agent.Name, err)
	}

	return nil
}

// Update rewrites an existing agent's editable and runtime fields, keyed by its
// stable id.
func (s *Store) Update(ctx context.Context, agent Agent) error {
	if err := s.queries.UpdateAgent(ctx, models.UpdateAgentParams{
		ID:              agent.ID,
		Cwd:             agent.Cwd,
		Flags:           agent.Flags,
		Prompt:          agent.Prompt,
		PaneID:          agent.Pane,
		State:           agent.State,
		ClaudeSessionID: agent.ClaudeSessionID,
		WorkflowID:      agent.WorkflowID,
	}); err != nil {
		return fmt.Errorf("failed to update agent %q: %w", agent.Name, err)
	}

	return nil
}

// SetStateByID updates just an agent's state, keyed by its stable id. The
// reconciler uses this so a write decided from a snapshot always lands on the
// exact row it observed, even if that row's name was reused by another agent in
// the meantime.
func (s *Store) SetStateByID(ctx context.Context, id, state string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.SetAgentStateByID(ctx, models.SetAgentStateByIDParams{ID: id, State: state}); err != nil {
		return fmt.Errorf("failed to set state for id %q: %w", id, err)
	}

	return nil
}

// ClearPane drops the pane an agent claims to be running in, keyed by its stable
// id. A pane id is only unique while the tmux server that issued it lives, so a
// dormant agent still holding one would go on claiming an id the next server hands
// to a different agent. The reconciler calls this the moment it sees a pane that is
// not this agent's, which is what keeps a stale claim from outliving one pass.
func (s *Store) ClearPane(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.ClearAgentPane(ctx, id); err != nil {
		return fmt.Errorf("failed to clear pane for id %q: %w", id, err)
	}

	return nil
}

// SetMark puts a mark on an agent, keyed by id, and a nil mark clears it. It
// reports whether a row was touched, which is how a caller tells an id that matches
// nothing from one that does.
func (s *Store) SetMark(ctx context.Context, id string, mark *string) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	updated, err := s.queries.SetAgentMark(ctx, models.SetAgentMarkParams{ID: id, Mark: mark})
	if err != nil {
		return false, fmt.Errorf("failed to mark agent %q: %w", id, err)
	}

	return len(updated) > 0, nil
}

// SetHidden hides an agent from the panel's list or brings it back, keyed by id.
// It reports whether a row was touched, so an id that matches nothing is
// distinguishable from a flag that already held the value. An archived agent has no
// listing to be hidden from, so it is not a row this writes.
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

// ErrTooManyMetadataKeys reports that an agent already holds as many keys as it is
// allowed. It is a sentinel rather than a plain failure because the caller has to
// tell the agent it has hit a ceiling, which it can act on, apart from a write that
// genuinely broke.
var ErrTooManyMetadataKeys = errors.New("too many metadata keys")

// maxMetadataKeys caps how many facts one agent may record. The keys are the
// agent's own invention, so nothing bounds them on its own and a looping agent
// would fill the table. The limit is checked here rather than in the proto, since
// deciding it needs a count of what is already stored and a validation rule has
// only the request.
const maxMetadataKeys = 32

// SetMetadataParams identifies the fact being recorded. A nil Value clears the key
// rather than storing an empty one, so one call covers both writing a fact and
// retracting it.
type SetMetadataParams struct {
	ID    string
	Key   string
	Value *string
}

// SetMetadata records one fact an agent found out about its own work, replacing
// whatever that key held and leaving its other keys untouched. It reports whether a
// row was written, which is how a caller tells an id matching no live agent from a
// key that was already absent.
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

	// What the agent already holds is read before the write rather than counted after
	// it, so the ceiling refuses a new key instead of admitting it and reporting a
	// failure the agent cannot undo. A key it already holds is a revision and passes
	// however full it is, since correcting a fact should not be what runs it out of
	// room.
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

// GetByIdempotencyKey returns the agent recorded under a caller's key, or nil
// when none is. It spans archived agents deliberately. The key exists so a retry
// finds what it already created, and an agent archived since is still that agent,
// so returning nil here would spawn a duplicate.
func (s *Store) GetByIdempotencyKey(ctx context.Context, key string) (*Agent, error) {
	if key == "" {
		return nil, nil
	}

	row, err := s.queries.GetAgentByIdempotencyKey(ctx, &key)
	if store.NoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the agent for idempotency key %q: %w", key, err)
	}

	agent := agentFromRow(*row)

	return &agent, nil
}

// CountLiveForSchedule reports how many agents a schedule has that are still
// working on something, which is what tells a firing that the last one is still
// going.
//
// An agent sitting on a confirmation prompt counts: it is unfinished work someone
// still has to answer, and starting a second run beside it would only add another.
// An agent recorded but never yet seen working does not, since that is the state a
// launch that died the moment it was typed keeps for good, and counting it would
// leave the schedule blocked on a run that is never coming.
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

// Delete removes an agent, keyed by id so it reaches the archived rows the trash
// destroys. The transcript keyed to the same id is the transcript package's to
// remove, in the same transaction, since an agent deleted without its transcript
// is a half-state worth avoiding.
func (s *Store) Delete(ctx context.Context, id string) error {
	if err := s.queries.DeleteAgentByID(ctx, id); err != nil {
		return fmt.Errorf("failed to delete agent %q: %w", id, err)
	}

	return nil
}

// ErrNameTaken reports that a rename collided with a live agent's name. The
// unique index is scoped to live rows, so this only ever means an agent currently
// listed answers to that name.
var ErrNameTaken = errors.New("name already taken")

// uniqueViolation is the SQLSTATE Postgres raises for a broken unique constraint,
// which is how a taken name is told apart from a real failure without reading the
// driver's message text.
const uniqueViolation = "23505"

// Rename changes an agent's display name, keyed by id. It reports whether a row
// was renamed, so an id that matches nothing is distinguishable from a rename to
// the name an agent already had.
func (s *Store) Rename(ctx context.Context, id, name string) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	renamed, err := s.queries.RenameAgent(ctx, models.RenameAgentParams{ID: id, Name: name})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return false, ErrNameTaken
		}

		return false, fmt.Errorf("failed to rename agent %q: %w", id, err)
	}

	return len(renamed) > 0, nil
}

// Archive hides an agent from listings without dropping the row.
func (s *Store) Archive(ctx context.Context, name string) error {
	if err := s.queries.MarkAgentDeleted(ctx, name); err != nil {
		return fmt.Errorf("failed to archive agent %q: %w", name, err)
	}

	return nil
}

// Restore clears an agent's archived flag by id, so an agent found in search
// returns to the normal listing. It is keyed by id, not name, because an
// archived row can share a name with a live agent, so the id is the only
// unambiguous handle to the exact row to restore.
//
// It clears the hidden flag with it. Restoring is a request for the agent to be back
// in the list, and one that came back still hidden would take no row, which reads as
// a restore that did nothing.
func (s *Store) Restore(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.RestoreAgent(ctx, id); err != nil {
		return fmt.Errorf("failed to restore agent %q: %w", id, err)
	}

	return nil
}

// ArchiveWorkflowAgents archives every live agent in a workflow, keeping their
// workflow id so they still list under it in search.
func (s *Store) ArchiveWorkflowAgents(ctx context.Context, workflowID string) error {
	if err := s.queries.MarkWorkflowAgentsDeleted(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to archive agents of workflow %q: %w", workflowID, err)
	}

	return nil
}

// agentFromRow builds an Agent from its row.
func agentFromRow(agent models.Agent) Agent {
	return Agent{
		ID:              agent.ID,
		Name:            agent.Name,
		Pane:            agent.PaneID,
		Cwd:             agent.Cwd,
		Flags:           agent.Flags,
		Prompt:          agent.Prompt,
		State:           agent.State,
		ClaudeSessionID: agent.ClaudeSessionID,
		WorkflowID:      agent.WorkflowID,
		ScheduleID:      agent.ScheduleID,
		IdempotencyKey:  agent.IdempotencyKey,
		Deleted:         agent.IsDeleted,
		Hidden:          agent.IsHidden,
		Mark:            agent.Mark,
		CreatedAt:       agent.CreatedAt,
		UpdatedAt:       agent.UpdatedAt,
	}
}
