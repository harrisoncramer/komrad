package scriptrun

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"komrade/config"
	"komrade/internal/logging"
	"komrade/internal/scripts"
	"komrade/internal/store"

	runstore "komrade/internal/store/run"
)

// maxLogRunAttempts bounds the walk up through numbers a second process has already taken.
const maxLogRunAttempts = 64

type runRecorder interface {
	Begin(ctx context.Context, params runstore.BeginParams) (string, error)
	Finish(ctx context.Context, params runstore.FinishParams) error
	Abandon(ctx context.Context, id string) error
	Prune(ctx context.Context, agentID string, keep int) error
}

// runRecord is one run as komrade is watching it.
type runRecord struct {
	logPath string
	id      string
}

// claimRunLog names the file one run writes to and creates it empty, taking the number under
// the lock and by an exclusive create.
func (s *ScriptService) claimRunLog(ctx context.Context, agentID, trigger, name string) (string, int) {
	if s.logsDir == "" || agentID == "" {
		return "", 0
	}

	s.logRunMu.Lock()
	defer s.logRunMu.Unlock()

	dir := config.RunLogDir(s.logsDir, agentID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to make an agent's run log directory",
			append([]slog.Attr{slog.String(logging.FilePath, dir)}, logging.Err(err)...)...)

		return "", 0
	}

	number := 1
	if runs := config.ListRunLogs(s.logsDir, agentID); len(runs) > 0 {
		number = runs[0].Number + 1
	}

	for range maxLogRunAttempts {
		path := filepath.Join(dir, config.RunLogName(number, trigger, name))

		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, fs.ErrExist) {
			number++

			continue
		}
		if err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to open a run log",
				append([]slog.Attr{slog.String(logging.FilePath, path)}, logging.Err(err)...)...)

			return "", 0
		}
		if closeErr := file.Close(); closeErr != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to close a run log",
				append([]slog.Attr{slog.String(logging.FilePath, path)}, logging.Err(closeErr)...)...)
		}

		pruneRunLogs(ctx, s.logsDir, agentID)

		return path, number
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to name a run of a script",
		slog.String(logging.AgentID, agentID),
		slog.String(logging.ScriptName, name))

	return "", 0
}

// beginRun claims a run's log file and opens the row that indexes it.
func (s *ScriptService) beginRun(ctx context.Context, agentID, trigger, name string) runRecord {
	path, number := s.claimRunLog(ctx, agentID, trigger, name)
	record := runRecord{logPath: path}

	if s.runs == nil || agentID == "" {
		return record
	}

	var logNumber *int32
	if number > 0 {
		claimed := int32(number)
		logNumber = &claimed
	}

	id, err := s.runs.Begin(ctx, runstore.BeginParams{
		AgentID:   agentID,
		Trigger:   trigger,
		Script:    name,
		LogNumber: logNumber,
	})
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to record a run of a script",
			append([]slog.Attr{
				slog.String(logging.AgentID, agentID),
				slog.String(logging.ScriptName, name),
			}, logging.Err(err)...)...)

		return record
	}
	record.id = id

	// Bounded by the log-file count so a row never outlives the output it points at.
	if err := s.runs.Prune(ctx, agentID, config.MaxRunLogs); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to prune an agent's runs",
			append([]slog.Attr{slog.String(logging.AgentID, agentID)}, logging.Err(err)...)...)
	}

	return record
}

// failRun closes the row of a run that failed, putting the reason in its log when the script
// itself said nothing.
func (s *ScriptService) failRun(ctx context.Context, record runRecord, result scripts.Result, cause error) {
	noteEmptyLog(ctx, record.logPath, cause)
	s.finishRun(ctx, record, result, cause)
}

// finishRun closes a run's row with what the script exited on and why it failed, if it did.
func (s *ScriptService) finishRun(ctx context.Context, record runRecord, result scripts.Result, cause error) {
	if s.runs == nil || record.id == "" {
		return
	}

	if err := s.runs.Finish(ctx, runstore.FinishParams{
		ID:       record.id,
		ExitCode: runExitCode(result),
		Failure:  store.FailureReason(cause),
	}); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to close the record of a run",
			append([]slog.Attr{slog.String(logging.RunID, record.id)}, logging.Err(err)...)...)
	}
}

// abandonRun drops the row of a run that never started, alongside the empty log it claimed.
func (s *ScriptService) abandonRun(ctx context.Context, record runRecord) {
	if s.runs == nil || record.id == "" {
		return
	}

	if err := s.runs.Abandon(ctx, record.id); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to drop the record of a run that never started",
			append([]slog.Attr{slog.String(logging.RunID, record.id)}, logging.Err(err)...)...)
	}
}

func runExitCode(result scripts.Result) *int32 {
	if result.ExitCode < 0 {
		return nil
	}

	code := int32(result.ExitCode)

	return &code
}

// pruneRunLogs drops the runs past the last MaxRunLogs.
func pruneRunLogs(ctx context.Context, logsDir, agentID string) {
	runs := config.ListRunLogs(logsDir, agentID)
	if len(runs) <= config.MaxRunLogs {
		return
	}

	for _, run := range runs[config.MaxRunLogs:] {
		if err := os.Remove(run.Path); err != nil && !os.IsNotExist(err) {
			logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to prune a run log",
				append([]slog.Attr{slog.String(logging.FilePath, run.Path)}, logging.Err(err)...)...)
		}
	}
}

// noteEmptyLog puts the reason into a log its run never wrote a byte to.
func noteEmptyLog(ctx context.Context, path string, cause error) {
	if path == "" {
		return
	}

	if info, err := os.Stat(path); err != nil || info.Size() > 0 {
		return
	}

	writeRunLog(ctx, path, []byte(cause.Error()+"\n"))
}

func writeRunLog(ctx context.Context, path string, content []byte) {
	if path == "" {
		return
	}

	if err := os.WriteFile(path, content, 0o644); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to write run log",
			append([]slog.Attr{slog.String(logging.FilePath, path)}, logging.Err(err)...)...)
	}
}

// removeEmptyLog drops the file a run claimed but never wrote to.
func removeEmptyLog(ctx context.Context, path string) {
	if path == "" {
		return
	}

	info, err := os.Stat(path)
	if err != nil || info.Size() > 0 {
		return
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to remove the log of a run that never started",
			append([]slog.Attr{slog.String(logging.FilePath, path)}, logging.Err(err)...)...)
	}
}
