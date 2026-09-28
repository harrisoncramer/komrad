// Package log serves the service's own log back, by the trace id its lines carry.
package log

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"

	"komrad/config"
	"komrad/internal/fail"
	"komrad/internal/logging"
	"komrad/internal/logread"
	"komrad/internal/rpcerr"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	logv1 "komrad/proto/gen/log/v1"

	"google.golang.org/protobuf/types/known/structpb"
)

// defaultAgentLogLimit is how many lines a read of agent logs answers when the caller names no count.
const defaultAgentLogLimit = 200

// serviceSource is the source that reads the service log.
const serviceSource = "service"

var (
	errBadCursor      = fail.InvalidArgument("the cursor is not one this service answered")
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
	List(agentID string) []config.RunLog
	Newest(ctx context.Context, run logread.Run, query logread.Query) (*logread.Window, error)
	Since(ctx context.Context, run logread.Run, offset int64, query logread.Query) (*logread.Window, error)
}

type Handler struct {
	log  logReader
	runs runReader
}

// NewHandler constructs the handler over the service log and the script runs agents recorded.
func NewHandler(log logReader, runs runReader) *Handler {
	return &Handler{log: log, runs: runs}
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
			Time:    line.Time,
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
			StartedAt: summary.StartedAt,
			What:      summary.What,
			Lines:     int32(summary.Lines),
		}
	})

	return &logv1.ListRecentTracesResponse{Traces: resources}, nil
}

// ReadAgentLogs is the lines of one of the agents' log sources, the newest without a cursor and
// those written since it with one.
func (s *Handler) ReadAgentLogs(ctx context.Context, req *logv1.ReadAgentLogsRequest) (*logv1.ReadAgentLogsResponse, *fail.Error) {
	query := logread.Query{Limit: int(req.GetLimit())}
	if query.Limit == 0 {
		query.Limit = defaultAgentLogLimit
	}

	offset := int64(-1)
	if req.GetCursor() != "" {
		parsed, err := strconv.ParseInt(req.GetCursor(), 10, 64)
		if err != nil || parsed < 0 {
			return nil, errBadCursor.Saying("cursor %q is not one this service answered", req.GetCursor())
		}
		offset = parsed
	}

	if req.GetPattern() != "" {
		pattern, err := regexp.Compile(req.GetPattern())
		if err != nil {
			return nil, errBadPattern.Saying("pattern %q does not compile: %v", req.GetPattern(), err)
		}
		query.Pattern = pattern
	}

	isRun := req.GetSource() != "" && req.GetSource() != serviceSource
	if isRun && len(req.GetAgentIds()) != 1 {
		return nil, errOneAgentPerRun.Saying("source %q was asked of %d agents", req.GetSource(), len(req.GetAgentIds()))
	}

	var (
		window *logread.Window
		err    error
	)
	switch {
	case isRun && offset < 0:
		window, err = s.runs.Newest(ctx, logread.Run{AgentID: req.GetAgentIds()[0], Name: req.GetSource()}, query)
	case isRun:
		window, err = s.runs.Since(ctx, logread.Run{AgentID: req.GetAgentIds()[0], Name: req.GetSource()}, offset, query)
	case offset < 0:
		window, err = s.log.NewestAboutAgents(ctx, req.GetAgentIds(), query)
	default:
		window, err = s.log.AboutAgentsSince(ctx, req.GetAgentIds(), offset, query)
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

// ListAgentLogSources is the service log and then each script run the agent recorded, newest first.
func (s *Handler) ListAgentLogSources(_ context.Context, req *logv1.ListAgentLogSourcesRequest) (*logv1.ListAgentLogSourcesResponse, *fail.Error) {
	runs := xslice.Map(s.runs.List(req.GetAgentId()), func(run config.RunLog) *logv1.LogSource {
		return &logv1.LogSource{
			Id:   filepath.Base(run.Path),
			Name: fmt.Sprintf("%03d %s %s", run.Number, run.Trigger, run.Script),
			Run:  xptr.Ptr(int32(run.Number)),
		}
	})

	return &logv1.ListAgentLogSourcesResponse{
		Sources: slices.Concat([]*logv1.LogSource{{Id: serviceSource, Name: serviceSource}}, runs),
	}, nil
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
