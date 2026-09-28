// Package log serves the service's own log back, by the trace id its lines carry.
package log

import (
	"context"
	"strconv"

	"komrad/internal/fail"
	"komrad/internal/logging"
	"komrad/internal/logread"
	"komrad/internal/rpcerr"
	"komrad/lib/xslice"
	logv1 "komrad/proto/gen/log/v1"

	"google.golang.org/protobuf/types/known/structpb"
)

// defaultAgentLogLimit is how many lines a read of agent logs answers when the caller names no count.
const defaultAgentLogLimit = 200

var errBadCursor = fail.InvalidArgument("the cursor is not one this service answered")

type logReader interface {
	Lines(trace string) ([]logread.Line, error)
	RecentTraces(limit int) ([]logread.Summary, error)
	NewestAboutAgents(ctx context.Context, agentIDs []string, limit int) (logread.Window, error)
	AboutAgentsSince(ctx context.Context, agentIDs []string, offset int64, limit int) (logread.Window, error)
}

type Handler struct {
	log logReader
}

// NewHandler constructs the handler over the log reader.
func NewHandler(log logReader) *Handler {
	return &Handler{log: log}
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

// ReadAgentLogs is the lines about some agents, the newest without a cursor and those written since
// it with one.
func (s *Handler) ReadAgentLogs(ctx context.Context, req *logv1.ReadAgentLogsRequest) (*logv1.ReadAgentLogsResponse, *fail.Error) {
	limit := int(req.GetLimit())
	if limit == 0 {
		limit = defaultAgentLogLimit
	}

	var (
		window logread.Window
		err    error
	)
	if req.GetCursor() == "" {
		window, err = s.log.NewestAboutAgents(ctx, req.GetAgentIds(), limit)
	} else {
		offset, parseErr := strconv.ParseInt(req.GetCursor(), 10, 64)
		if parseErr != nil || offset < 0 {
			return nil, errBadCursor.Saying("cursor %q is not one this service answered", req.GetCursor())
		}

		window, err = s.log.AboutAgentsSince(ctx, req.GetAgentIds(), offset, limit)
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
