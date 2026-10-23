package log

import (
	"context"
	"regexp"
	"slices"
	"strconv"

	"komrad/config"
	"komrad/internal/fail"
	"komrad/internal/logging"
	"komrad/internal/logread"
	"komrad/internal/rpcerr"
	runstore "komrad/internal/store/run"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/dates"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	eventv1 "komrad/proto/gen/event/v1"
	logv1 "komrad/proto/gen/log/v1"
	"komrad/proto/gen/log/v1/logv1komrad"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
)

// defaultAgentLogLimit is how many lines a read of agent logs answers when the caller names no count.
const defaultAgentLogLimit = 200

// serverSource is the source that reads the server log.
const serverSource = "server"

var (
	errBadCursor      = fail.InvalidArgument("the cursor is not one this server answered")
	errOneAgentPerRun = fail.InvalidArgument("a script run's source is read for one agent at a time")
	errBadPattern     = fail.InvalidArgument("the pattern is not a regular expression")
)

type logReader interface {
	Lines(trace string) ([]logread.Line, error)
	RecentTraces(limit int) ([]logread.Summary, error)
	NewestAboutAgents(ctx context.Context, agentIDs []string, query logread.Query) (*logread.Window, error)
	AboutAgentsSince(ctx context.Context, agentIDs []string, offset int64, query logread.Query) (*logread.Window, error)
}

type runReader interface {
	List(agentID uuid.UUID) []config.RunLog
	Newest(ctx context.Context, run logread.Run, query logread.Query) (*logread.Window, error)
	Since(ctx context.Context, run logread.Run, offset int64, query logread.Query) (*logread.Window, error)
}

type taskRunLister interface {
	ListRuns(ctx context.Context, taskID uuid.UUID) ([]runstore.Run, error)
}

type Handler struct {
	log      logReader
	runs     runReader
	taskRuns taskRunLister
}

// NewHandler constructs the handler over the server log, the script runs agents recorded, and the record of which ran on each task.
func NewHandler(log logReader, runs runReader, taskRuns taskRunLister) *Handler {
	return &Handler{
		log:      log,
		runs:     runs,
		taskRuns: taskRuns,
	}
}

// GetLogLines is every line written under one trace id.
func (s *Handler) GetLogLines(_ context.Context, req *logv1.GetLogLinesRequest) (*logv1.GetLogLinesResponse, *fail.Error) {
	lines, err := s.log.Lines(req.GetTraceId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the log")
	}

	resources := make([]*logv1.LogLine, 0, len(lines))
	for _, line := range lines {
		fields, err := structpb.NewStruct(plain(line.Fields))
		if err != nil {
			return nil, rpcerr.From(err, "failed to carry a log line's fields")
		}

		resources = append(resources, &logv1.LogLine{
			Time:    dates.Stamp(line.Time),
			Level:   line.Level,
			Message: line.Message,
			Fields:  fields,
		})
	}

	return &logv1.GetLogLinesResponse{Lines: resources}, nil
}

// ListRecentTraces is the trace ids the log ends with.
func (s *Handler) ListRecentTraces(_ context.Context, req *logv1.ListRecentTracesRequest) (*logv1.ListRecentTracesResponse, *fail.Error) {
	summaries, err := s.log.RecentTraces(int(req.GetLimit()))
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the traces")
	}

	resources := xslice.Map(summaries, func(summary logread.Summary) *logv1.TraceSummary {
		return &logv1.TraceSummary{
			TraceId:   summary.Trace,
			StartedAt: dates.Stamp(summary.StartedAt),
			What:      summary.What,
			Lines:     int32(summary.Lines),
		}
	})

	return &logv1.ListRecentTracesResponse{Traces: resources}, nil
}

// ReadAgentLogs is the lines of one of the agents' log sources, the newest without a cursor and
// those written since it with one.
func (s *Handler) ReadAgentLogs(ctx context.Context, req *logv1komrad.ReadAgentLogsRequest) (*logv1.ReadAgentLogsResponse, *fail.Error) {
	agentIDs := xslice.Map(req.AgentIds, uuid.UUID.String)
	query := logread.Query{Limit: int(req.Limit)}
	if query.Limit == 0 {
		query.Limit = defaultAgentLogLimit
	}

	offset := int64(-1)
	if req.Cursor != "" {
		parsed, err := strconv.ParseInt(req.Cursor, 10, 64)
		if err != nil || parsed < 0 {
			return nil, errBadCursor.Saying("cursor %q is not one this server answered", req.Cursor)
		}
		offset = parsed
	}

	if req.Pattern != "" {
		pattern, err := regexp.Compile(req.Pattern)
		if err != nil {
			return nil, errBadPattern.Saying("pattern %q does not compile: %v", req.Pattern, err)
		}
		query.Pattern = pattern
	}

	isRun := req.Source != "" && req.Source != serverSource
	if isRun && len(agentIDs) != 1 {
		return nil, errOneAgentPerRun.Saying("source %q was asked of %d agents", req.Source, len(agentIDs))
	}

	var (
		window *logread.Window
		err    error
	)
	switch {
	case isRun && offset < 0:
		window, err = s.runs.Newest(ctx, logread.Run{AgentID: req.AgentIds[0], Name: req.Source}, query)
	case isRun:
		window, err = s.runs.Since(ctx, logread.Run{AgentID: req.AgentIds[0], Name: req.Source}, offset, query)
	case offset < 0:
		window, err = s.log.NewestAboutAgents(ctx, agentIDs, query)
	default:
		window, err = s.log.AboutAgentsSince(ctx, agentIDs, offset, query)
	}
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the agents' logs")
	}

	return &logv1.ReadAgentLogsResponse{
		Lines:  window.Lines,
		Cursor: strconv.FormatInt(window.Offset, 10),
		Reset_: window.Reset,
		More:   window.More,
	}, nil
}

// ListAgentLogSources is the server log and then each script run of the agent's own moments it recorded, newest first.
func (s *Handler) ListAgentLogSources(_ context.Context, req *logv1komrad.ListAgentLogSourcesRequest) (*logv1.ListAgentLogSourcesResponse, *fail.Error) {
	agentID := req.AgentId
	owned := xslice.Filter(s.runs.List(agentID), func(run config.RunLog) bool {
		return !workflowstore.IsTaskMoment(run.Trigger)
	})
	runs := xslice.Map(owned, func(run config.RunLog) *logv1.LogSource {
		return renderRunSource(renderRunSourceParams{
			agentID: agentID,
			number:  run.Number,
			trigger: run.Trigger,
			script:  run.Script,
		})
	})

	return &logv1.ListAgentLogSourcesResponse{
		Sources: slices.Concat([]*logv1.LogSource{{Id: serverSource}}, runs),
	}, nil
}

// ListTaskLogSources is each script run recorded on the task, by any agent on it, newest first.
func (s *Handler) ListTaskLogSources(ctx context.Context, req *logv1komrad.ListTaskLogSourcesRequest) (*logv1.ListTaskLogSourcesResponse, *fail.Error) {
	runs, err := s.taskRuns.ListRuns(ctx, req.TaskId)
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the task's runs")
	}

	logged := xslice.Filter(runs, func(run runstore.Run) bool { return run.LogNumber != nil })

	return &logv1.ListTaskLogSourcesResponse{
		Sources: xslice.Map(logged, func(run runstore.Run) *logv1.LogSource {
			source := renderRunSource(renderRunSourceParams{
				agentID: run.AgentID,
				number:  int(*run.LogNumber),
				trigger: run.Trigger,
				script:  run.Script,
			})
			source.Failed = run.Failure != ""

			return source
		}),
	}, nil
}

type renderRunSourceParams struct {
	agentID uuid.UUID
	number  int
	trigger eventv1.Moment
	script  string
}

// renderRunSource is the source one script run an agent recorded is read from.
func renderRunSource(params renderRunSourceParams) *logv1.LogSource {
	return &logv1.LogSource{
		Id:      config.RunLogName(params.number, params.trigger, params.script),
		Run:     xptr.Ptr(int32(params.number)),
		Trigger: params.trigger,
		Script:  params.script,
		AgentId: params.agentID.String(),
	}
}

// plain leaves out the fields the line already carries on its own, and anything a Struct
// cannot hold.
func plain(fields map[string]any) map[string]any {
	skip := map[string]bool{
		logging.TimestampKey: true, logging.SeverityKey: true, logging.BodyKey: true,
		"severity_number": true, logging.TraceID: true,
	}

	kept := make(map[string]any, len(fields))
	for key, value := range fields {
		if skip[key] {
			continue
		}
		if _, err := structpb.NewValue(value); err != nil {
			continue
		}

		kept[key] = value
	}

	return kept
}
