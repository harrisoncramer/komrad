// Package log serves the service's own log back, by the trace id its lines carry.
package log

import (
	"context"

	"komrade/internal/fail"
	"komrade/internal/logging"
	"komrade/internal/logread"
	"komrade/internal/rpcerr"
	"komrade/lib/xslice"
	logv1 "komrade/proto/gen/log/v1"

	"google.golang.org/protobuf/types/known/structpb"
)

type logReader interface {
	Lines(trace string) ([]logread.Line, error)
	RecentTraces(limit int) ([]logread.Summary, error)
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
