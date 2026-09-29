package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"komrad/lib/xhttp"
	logv1 "komrad/proto/gen/log/v1"
	"komrad/proto/gen/log/v1/logv1connect"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
)

const logTimeout = 30 * time.Second

func logCmd(state *app) *cobra.Command {
	var recent bool

	cmd := &cobra.Command{
		Use:   "log [trace-id]",
		Short: "Show the log lines the service wrote under one trace id",
		Long: `Show the log lines the service wrote under one trace id.

Every line the service logs carries a trace id, which is one id for one request end
to end or one pass of a background loop. A call that fails answers with its trace
id, so following what happened behind a failure is a matter of naming the id here.
The service reads its own log back, so this works against a remote engine the same
as against this machine.

Run with --recent instead of an id to list the trace ids the log ends with, which
is where to start when you know what you did but not what it was called.`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), logTimeout)
			defer cancel()

			addr, release, err := state.dialAPI(ctx)
			if err != nil {
				return err
			}
			defer release()

			if err := requireServer(ctx, addr, state.env.Addr); err != nil {
				return err
			}

			client := logv1connect.NewLogServiceClient(xhttp.NewClient(logTimeout), "http://"+addr)
			if recent || len(args) == 0 {
				return listRecentTraces(ctx, cmd.OutOrStdout(), client)
			}

			return showLogLines(ctx, cmd.OutOrStdout(), client, args[0])
		},
	}

	cmd.Flags().BoolVar(&recent, "recent", false, "list the trace ids the log ends with rather than showing one")

	return cmd
}

func showLogLines(ctx context.Context, out io.Writer, client logv1connect.LogServiceClient, trace string) error {
	resp, err := client.GetLogLines(ctx, connect.NewRequest(&logv1.GetLogLinesRequest{TraceId: trace}))
	if err != nil {
		return err
	}

	var written strings.Builder
	for _, line := range resp.Msg.GetLines() {
		written.WriteString(renderLogLine(line))
		written.WriteString("\n")
	}

	return writeOut(out, written.String())
}

func listRecentTraces(ctx context.Context, out io.Writer, client logv1connect.LogServiceClient) error {
	resp, err := client.ListRecentTraces(ctx, connect.NewRequest(&logv1.ListRecentTracesRequest{}))
	if err != nil {
		return err
	}

	traces := resp.Msg.GetTraces()
	if len(traces) == 0 {
		return writeOut(out, "nothing in the log carries a trace id yet\n")
	}

	var written strings.Builder
	for _, trace := range traces {
		written.WriteString(fmt.Sprintf("%s  %-36s  %-28s  %d line(s)\n",
			shortTime(trace.GetStartedAt()), trace.GetTraceId(), trace.GetWhat(), trace.GetLines()))
	}

	return writeOut(out, written.String())
}

func writeOut(out io.Writer, body string) error {
	if _, err := io.WriteString(out, body); err != nil {
		return fmt.Errorf("failed to write the log: %w", err)
	}

	return nil
}

func renderLogLine(line *logv1.LogLine) string {
	var out strings.Builder

	out.WriteString(shortTime(line.GetTime()))
	out.WriteString("  ")
	out.WriteString(fmt.Sprintf("%-5s", strings.ToLower(line.GetLevel())))
	out.WriteString("  ")
	out.WriteString(line.GetMessage())

	fields := line.GetFields().AsMap()
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	for _, key := range keys {
		out.WriteString(fmt.Sprintf("  %s=%v", key, fields[key]))
	}

	return out.String()
}

func shortTime(stamp string) string {
	if len(stamp) < 19 {
		return stamp
	}

	return stamp[11:19]
}
