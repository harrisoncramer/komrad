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
	"weave/lib/xslice"

	"github.com/jackc/pgx/v5"
)

var ErrTooManyMetadataKeys = errors.New("too many metadata keys")

const maxMetadataKeys = 32

const slotsLock int64 = 7_240_116

const (
	StateThinking        = "thinking"
	StateWaiting         = "waiting"
	StateIdle            = "idle"
	StateSettingUp       = "setting_up"
	StateFailedToCreate  = "failed_to_create"
	StateFailedToCleanup = "failed_to_cleanup"
)

func IsFailed(state string) bool {
	return state == StateFailedToCreate || state == StateFailedToCleanup
}

type Metadata struct {
	Key   string
	Value string
}

type Agent struct {
	ID             string
	Name           string
	Pane           string
	Cwd            string
	Flags          string
	SetupAction    *string
	CleanupAction  *string
	Prompt         string
	State          string
	StateChangedAt time.Time

	StateSeenAt      time.Time
	Harness          string
	SessionID        string
	WorkflowID       string
	ScheduleID       *string
	IdempotencyKey   *string
	ArchivedAt       *time.Time
	NudgedAt         *time.Time
	Hidden           bool
	Tag              *string
	Metadata         []Metadata
	SpawnedByAgentID *string
	CreatedAt        time.Time
	UpdatedAt        time.Time

	Branch *string

	// AdoptedBranch is the branch an agent was put on rather than one weave made for it,
	// so it is what was asked for where Branch is what the cwd is on now.
	AdoptedBranch string

	WorktreeRemoved bool

	InterruptedAt *time.Time
}

// SanitizeName maps a name to characters tmux accepts in session names.
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

type Store struct {
	queries *models.Queries
	db      store.DBTX
}

// New builds an agent store over a pool or transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db), db: db}
}

type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// List returns every persisted agent, newest first.
func (s *Store) List(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

type Cursor struct {
	ID string `json:"i"`
}

// Validate checks that the cursor contains an agent ID.
func (c Cursor) Validate() error {
	if !store.IsID(c.ID) {
		return fmt.Errorf("cursor id %q is not an id", c.ID)
	}

	return nil
}

// ListPage returns one page of live or archived agents, newest first.
func (s *Store) ListPage(ctx context.Context, limit int64, cursor *Cursor, archived bool) ([]Agent, error) {
	params := models.ListAgentsPageParams{Lim: int32(limit), Archived: archived}
	if cursor != nil {
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListAgentsPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of agents: %w", err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

func (s *Store) hydrateMetadataForAgents(ctx context.Context, rows []*models.Agent) ([]Agent, error) {
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

func (s *Store) hydrateMetadataForAgent(ctx context.Context, row *models.Agent) (*Agent, error) {
	agents, err := s.hydrateMetadataForAgents(ctx, []*models.Agent{row})
	if err != nil {
		return nil, err
	}

	return &agents[0], nil
}

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

	return s.hydrateMetadataForAgents(ctx, rows)
}

// GetInWorkflow returns a live agent by workflow and name.
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

	return s.hydrateMetadataForAgent(ctx, row)
}

// GetByID returns a live or archived agent by ID.
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

	return s.hydrateMetadataForAgent(ctx, row)
}

// ListByID returns live and archived agents matching the given IDs.
func (s *Store) ListByID(ctx context.Context, ids []string) ([]Agent, error) {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.GetAgentsByID(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to get %d agents: %w", len(storable), err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
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

// ActiveNameExists reports whether a live agent holds the name in a workflow.
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

// Create inserts an agent and assigns an ID when needed.
func (s *Store) Create(ctx context.Context, agent Agent) error {
	if agent.ID == "" {
		id, err := store.NewID()
		if err != nil {
			return err
		}

		agent.ID = id
	}

	if _, err := s.queries.InsertAgent(ctx, models.InsertAgentParams{
		ID:               agent.ID,
		Name:             agent.Name,
		Cwd:              agent.Cwd,
		Flags:            agent.Flags,
		Prompt:           agent.Prompt,
		PaneID:           agent.Pane,
		State:            agent.State,
		Harness:          agent.Harness,
		SessionID:        agent.SessionID,
		WorkflowID:       agent.WorkflowID,
		ScheduleID:       agent.ScheduleID,
		IdempotencyKey:   agent.IdempotencyKey,
		IsHidden:         agent.Hidden,
		SetupAction:      agent.SetupAction,
		CleanupAction:    agent.CleanupAction,
		SpawnedByAgentID: agent.SpawnedByAgentID,
		AdoptedBranch:    agent.AdoptedBranch,
	}); err != nil {
		return fmt.Errorf("failed to insert agent %q: %w", agent.Name, err)
	}

	return nil
}

// Update writes an agent's editable and runtime fields.
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

// UpdateIfLive rewrites an agent's row and reports whether it did, refusing one that an
// archive or a delete took while a resume was mid-flight.
func (s *Store) UpdateIfLive(ctx context.Context, agent Agent) (bool, error) {
	if !store.IsID(agent.ID) {
		return false, nil
	}

	updated, err := s.queries.UpdateAgentIfLive(ctx, models.UpdateAgentIfLiveParams{
		ID:         agent.ID,
		Cwd:        agent.Cwd,
		Flags:      agent.Flags,
		Prompt:     agent.Prompt,
		PaneID:     agent.Pane,
		State:      agent.State,
		SessionID:  agent.SessionID,
		WorkflowID: agent.WorkflowID,
	})
	if err != nil {
		return false, fmt.Errorf("failed to update agent %q: %w", agent.Name, err)
	}

	return len(updated) > 0, nil
}

// ClaimNudge takes the right to tell an agent about waiting mail, checking and stamping in
// one statement so two passes racing cannot both go on to type.
func (s *Store) ClaimNudge(ctx context.Context, id string, retryAfter time.Duration) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	claimed, err := s.queries.ClaimAgentNudge(ctx, models.ClaimAgentNudgeParams{
		ID:          id,
		RetryMicros: retryAfter.Microseconds(),
	})
	if err != nil {
		return false, fmt.Errorf("failed to claim the nudge for agent %q: %w", id, err)
	}

	return len(claimed) > 0, nil
}

// SetStateByID records the state a reconcile pass observed, stamping when it was seen
// whether or not the value moved.
func (s *Store) SetStateByID(ctx context.Context, id, state string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.SetAgentStateByID(ctx, models.SetAgentStateByIDParams{ID: id, State: state}); err != nil {
		return fmt.Errorf("failed to set state for id %q: %w", id, err)
	}

	return nil
}

// MarkSettingUpIfLive says a resume is in flight on one agent and reports whether it did,
// refusing an archived row so the resume notices rather than writing a pane onto a hidden one.
func (s *Store) MarkSettingUpIfLive(ctx context.Context, id string) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	marked, err := s.queries.MarkAgentSettingUpIfLive(ctx, id)
	if err != nil {
		return false, fmt.Errorf("failed to mark id %q as setting up: %w", id, err)
	}

	return len(marked) > 0, nil
}

// SetBranchByID records the branch checked out at an agent's cwd, or clears it when branch
// is nil, and is written only by the branch syncer's periodic pass.
func (s *Store) SetBranchByID(ctx context.Context, id string, branch *string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.SetAgentBranchByID(ctx, models.SetAgentBranchByIDParams{ID: id, Branch: branch}); err != nil {
		return fmt.Errorf("failed to set the branch for id %q: %w", id, err)
	}

	return nil
}

// SetWorktreeRemovedByID records whether an agent's cwd no longer exists on disk, written
// only by the worktree syncer's periodic pass.
func (s *Store) SetWorktreeRemovedByID(ctx context.Context, id string, removed bool) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.SetAgentWorktreeRemovedByID(ctx, models.SetAgentWorktreeRemovedByIDParams{ID: id, WorktreeRemoved: removed}); err != nil {
		return fmt.Errorf("failed to set worktree_removed for id %q: %w", id, err)
	}

	return nil
}

// SetInterruptedAtByID records when an agent's conversation was cut off part-way through a
// turn, which a resume reads to decide whether to say anything on the way back in.
func (s *Store) SetInterruptedAtByID(ctx context.Context, id string, at *time.Time) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.SetAgentInterruptedAtByID(ctx, models.SetAgentInterruptedAtByIDParams{ID: id, InterruptedAt: at}); err != nil {
		return fmt.Errorf("failed to set interrupted_at for id %q: %w", id, err)
	}

	return nil
}

// SetSessionID records an agent's conversation when none is already stored.
func (s *Store) SetSessionID(ctx context.Context, id, sessionID string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.SetAgentSessionID(ctx, models.SetAgentSessionIDParams{ID: id, SessionID: sessionID}); err != nil {
		return fmt.Errorf("failed to set the session for id %q: %w", id, err)
	}

	return nil
}

type ForgetPaneParams struct {
	ID            string
	Pane          string
	UpdatedAt     time.Time
	InterruptedAt *time.Time
}

// ForgetPane drops an agent's pane, recording an interrupted turn in the same write when
// InterruptedAt is set, and reports whether it did.
func (s *Store) ForgetPane(ctx context.Context, params ForgetPaneParams) (bool, error) {
	if !store.IsID(params.ID) {
		return false, nil
	}

	// The write is guarded on UpdatedAt rather than on the pane, since a tmux server hands out
	// pane ids from a counter that restarts with it and a restart is when weave resumes every
	// agent at once.
	forgotten, err := s.queries.ForgetAgentPane(ctx, models.ForgetAgentPaneParams{
		ID:            params.ID,
		PaneID:        params.Pane,
		UpdatedAt:     params.UpdatedAt,
		InterruptedAt: params.InterruptedAt,
	})
	if err != nil {
		return false, fmt.Errorf("failed to forget the pane for id %q: %w", params.ID, err)
	}

	return len(forgotten) > 0, nil
}

// AgentPane names a pane and the agent whose it is, which is all that killing one takes: the
// owner has to be checked first, so a pane is never carried around without it.
type AgentPane struct {
	AgentID string
	Pane    string
}

type ArchiveIfUnchangedParams struct {
	ID string

	UpdatedAt time.Time
}

// ArchiveIfUnchanged archives one agent only while its row still reads as it was observed,
// so an archive decided from an older read cannot hide an agent a resume has brought back.
func (s *Store) ArchiveIfUnchanged(ctx context.Context, params ArchiveIfUnchangedParams) (bool, error) {
	if !store.IsID(params.ID) {
		return false, nil
	}

	archived, err := s.queries.MarkAgentArchivedIfUnchanged(ctx, models.MarkAgentArchivedIfUnchangedParams{
		ID:        params.ID,
		UpdatedAt: params.UpdatedAt,
	})
	if err != nil {
		return false, fmt.Errorf("failed to archive id %q: %w", params.ID, err)
	}

	return len(archived) > 0, nil
}

// SetTags updates the tag on the given agents and returns the number changed.
func (s *Store) SetTags(ctx context.Context, ids []string, tag *string) (int, error) {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return 0, nil
	}

	updated, err := s.queries.SetAgentTags(ctx, models.SetAgentTagsParams{Ids: storable, Tag: tag})
	if err != nil {
		return 0, fmt.Errorf("failed to tag %d agents: %w", len(storable), err)
	}

	return len(updated), nil
}

// SetHidden updates a live agent's hidden state.
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

type SetMetadataParams struct {
	ID    string
	Key   string
	Value *string
}

// SetMetadata sets or clears one metadata value and reports whether a row changed, taking
// an archived agent the same as a live one since the archive itself is recorded as metadata.
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

	beginner, ok := s.db.(txBeginner)
	if !ok {
		return false, fmt.Errorf("agent store has no transaction-capable connection to record metadata %q on agent %q", params.Key, params.ID)
	}

	tx, err := beginner.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to begin metadata transaction for agent %q: %w", params.ID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := models.New(tx)

	if err := queries.LockAgentMetadata(ctx, params.ID); err != nil {
		return false, fmt.Errorf("failed to lock metadata for agent %q: %w", params.ID, err)
	}

	held, err := queries.ListAgentMetadata(ctx, []string{params.ID})
	if err != nil {
		return false, fmt.Errorf("failed to read metadata on agent %q: %w", params.ID, err)
	}

	revising := slices.ContainsFunc(held, func(row *models.ListAgentMetadataRow) bool {
		return row.Key == params.Key
	})
	if len(held) >= maxMetadataKeys && !revising {
		return false, ErrTooManyMetadataKeys
	}

	written, err := queries.SetAgentMetadata(ctx, models.SetAgentMetadataParams{
		AgentID: params.ID,
		Key:     params.Key,
		Value:   *params.Value,
	})
	if err != nil {
		return false, fmt.Errorf("failed to record metadata %q on agent %q: %w", params.Key, params.ID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("failed to commit metadata for agent %q: %w", params.ID, err)
	}

	return len(written) > 0, nil
}

// GetByIdempotencyKey returns the live or archived agent recorded under a key.
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

	return s.hydrateMetadataForAgent(ctx, row)
}

// ReleaseIdempotencyKey takes a key back off the failed create holding it and reports whether
// it did, checking the state in the statement that clears the key so a caller which read the
// row a moment ago cannot free one off an agent that has since launched.
func (s *Store) ReleaseIdempotencyKey(ctx context.Context, key string) (bool, error) {
	if key == "" {
		return false, nil
	}

	released, err := s.queries.ReleaseAgentIdempotencyKey(ctx, &key)
	if err != nil {
		return false, fmt.Errorf("failed to release idempotency key %q: %w", key, err)
	}

	return len(released) > 0, nil
}

// CountLive returns the number of unarchived agents.
func (s *Store) CountLive(ctx context.Context) (int64, error) {
	count, err := s.queries.CountLiveAgents(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to count the live agents: %w", err)
	}

	return count, nil
}

// LockSlots serializes agent slot claims for the current transaction.
func (s *Store) LockSlots(ctx context.Context) error {
	if err := s.queries.LockAgentSlots(ctx, slotsLock); err != nil {
		return fmt.Errorf("failed to lock the agent slots: %w", err)
	}

	return nil
}

// CountLiveForSchedule returns the number of non-idle agents launched by a schedule.
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

// NamesHoldingSchedule returns the names of the agents outside a workflow that still point
// at a schedule, which is only the ones a schedule moved between workflows left behind.
func (s *Store) NamesHoldingSchedule(ctx context.Context, scheduleID, workflowID string) ([]string, error) {
	if !store.IsID(scheduleID) || !store.IsID(workflowID) {
		return nil, nil
	}

	names, err := s.queries.ListScheduleAgentNamesOutsideWorkflow(ctx, models.ListScheduleAgentNamesOutsideWorkflowParams{
		ScheduleID: &scheduleID,
		WorkflowID: workflowID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list the agents of schedule %q: %w", scheduleID, err)
	}

	return names, nil
}

// Delete removes live or archived agents by ID.
func (s *Store) Delete(ctx context.Context, ids []string) ([]AgentPane, error) {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	deleted, err := s.queries.DeleteAgentsByID(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to delete %d agents: %w", len(storable), err)
	}

	return xslice.Map(deleted, func(row *models.DeleteAgentsByIDRow) AgentPane {
		return AgentPane{AgentID: row.ID, Pane: row.PaneID}
	}), nil
}

// Rename changes an agent's display name and reports whether a row changed.
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

// Archive moves agents out of live listings, returning the pane each row held as it was
// archived rather than the one the caller last read.
func (s *Store) Archive(ctx context.Context, ids []string) ([]AgentPane, error) {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	// The pane is read under the lock the archive takes, since a resume or a create can
	// record one between the caller's read and this write.
	archived, err := s.queries.MarkAgentsArchived(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to archive %d agents: %w", len(storable), err)
	}

	return xslice.Map(archived, func(row *models.MarkAgentsArchivedRow) AgentPane {
		return AgentPane{AgentID: row.ID, Pane: row.PaneID}
	}), nil
}

// Restore returns archived agents to live listings.
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

// ArchiveWorkflowAgents archives every live agent in a workflow, returning the pane each row
// held as it was archived, for the same reason Archive does.
func (s *Store) ArchiveWorkflowAgents(ctx context.Context, workflowID string) ([]AgentPane, error) {
	archived, err := s.queries.MarkWorkflowAgentsArchived(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to archive agents of workflow %q: %w", workflowID, err)
	}

	return xslice.Map(archived, func(row *models.MarkWorkflowAgentsArchivedRow) AgentPane {
		return AgentPane{AgentID: row.ID, Pane: row.PaneID}
	}), nil
}

// RestoreWorkflowAgents restores every archived agent in a workflow.
func (s *Store) RestoreWorkflowAgents(ctx context.Context, workflowID string) error {
	if err := s.queries.MarkWorkflowAgentsRestored(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to restore agents of workflow %q: %w", workflowID, err)
	}

	return nil
}

// ListLiveInWorkflow returns the live agents in a workflow.
func (s *Store) ListLiveInWorkflow(ctx context.Context, workflowID string) ([]Agent, error) {
	rows, err := s.queries.ListLiveWorkflowAgents(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list live agents of workflow %q: %w", workflowID, err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

// ListArchivedInWorkflow returns the archived agents in a workflow.
func (s *Store) ListArchivedInWorkflow(ctx context.Context, workflowID string) ([]Agent, error) {
	rows, err := s.queries.ListArchivedWorkflowAgents(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list archived agents of workflow %q: %w", workflowID, err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

func keepStorableIDs(ids []string) []string {
	return xslice.Filter(ids, store.IsID)
}

func agentFromRow(agent models.Agent) Agent {
	return Agent{
		ID:               agent.ID,
		Name:             agent.Name,
		Pane:             agent.PaneID,
		Cwd:              agent.Cwd,
		Flags:            agent.Flags,
		SetupAction:      agent.SetupAction,
		CleanupAction:    agent.CleanupAction,
		Prompt:           agent.Prompt,
		State:            agent.State,
		StateChangedAt:   agent.StateChangedAt,
		StateSeenAt:      agent.StateSeenAt,
		Harness:          agent.Harness,
		SessionID:        agent.SessionID,
		WorkflowID:       agent.WorkflowID,
		ScheduleID:       agent.ScheduleID,
		IdempotencyKey:   agent.IdempotencyKey,
		ArchivedAt:       agent.ArchivedAt,
		NudgedAt:         agent.NudgedAt,
		Hidden:           agent.IsHidden,
		Tag:              agent.Tag,
		SpawnedByAgentID: agent.SpawnedByAgentID,
		CreatedAt:        agent.CreatedAt,
		UpdatedAt:        agent.UpdatedAt,
		Branch:           agent.Branch,
		AdoptedBranch:    agent.AdoptedBranch,
		WorktreeRemoved:  agent.WorktreeRemoved,
		InterruptedAt:    agent.InterruptedAt,
	}
}
