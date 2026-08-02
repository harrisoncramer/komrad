package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"weave/internal/caller"
	"weave/internal/rpcapi"

	"github.com/spf13/cobra"
)

// rpcTimeout bounds one call, sized for creating an agent, which opens a tmux window and
// types into it.
const rpcTimeout = 30 * time.Second

// rpcCmd is the parent of one subcommand per exposed RPC.
func rpcCmd(state *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rpc",
		Short: "Call the service's endpoints, one subcommand per RPC",
		Long: "Call the service's endpoints from a script.\n\n" +
			"Every subcommand here is one RPC, named as the MCP tool surface names it, so a\n" +
			"script and a model ask for the same operation by the same name. Request fields are\n" +
			"flags, and the response is the API's own JSON on stdout.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	for exposed := range rpcapi.GetMethods() {
		cmd.AddCommand(rpcMethodCmd(state, exposed))
	}

	return cmd
}

// rpcMethodCmd builds the subcommand for one RPC, taking its name, help, and flags off the
// descriptor so nothing is written per method.
func rpcMethodCmd(state *app, exposed rpcapi.Method) *cobra.Command {
	var (
		requestJSON string
		compact     bool
		timeout     time.Duration
	)

	name := strings.ReplaceAll(exposed.Annotation.GetToolName(), "_", "-")
	description := strings.Join(strings.Fields(exposed.Annotation.GetDescription()), " ")

	cmd := &cobra.Command{
		Use:           name,
		Aliases:       []string{exposed.Annotation.GetToolName()},
		Short:         takeFirstSentence(description),
		Long:          description,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	fields := bindRequestFlags(cmd, exposed.Method.Input())
	if len(fields.skipped) > 0 {
		cmd.Long += "\n\nNo flag is derived for " + strings.Join(fields.skipped, ", ") +
			"; pass those with --request-json."
	}

	cmd.Flags().StringVar(&requestJSON, "request-json", "", "the whole request as JSON, which the flags above are laid over")
	cmd.Flags().BoolVar(&compact, "compact", false, "print the response on one line")
	cmd.Flags().DurationVar(&timeout, "timeout", rpcTimeout, "how long to wait for the call")

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		request, err := fields.request(cmd.Flags(), requestJSON)
		if err != nil {
			return err
		}

		if err := requireServer(state.env.Addr); err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()

		payload, err := callRPC(ctx, callRPCParams{
			Addr:    state.env.Addr,
			Path:    exposed.GetPath(),
			From:    caller.Caller{AgentID: state.env.AgentID, SessionID: state.env.SessionID},
			Request: request,
		})
		if err != nil {
			return err
		}

		return writeResponse(cmd.OutOrStdout(), payload, compact)
	}

	return cmd
}

// requireServer refuses the call when nothing is serving. Starting one as a side effect
// would leave a daemon behind whichever command happened to run first, so bringing the
// server up stays weave start's job.
func requireServer(addr string) error {
	if probeServer(addr).answering {
		return nil
	}

	return fmt.Errorf("nothing is serving at %s, run weave start first", addr)
}

// callRPCParams is one call: where the service is, which method, who is asking, and the
// request body.
type callRPCParams struct {
	Addr    string
	Path    string
	From    caller.Caller
	Request map[string]any
}

// callRPC posts the request to the service's Connect endpoint. Going over HTTP rather than
// through a generated client per service is what lets one function serve every RPC.
func callRPC(ctx context.Context, params callRPCParams) ([]byte, error) {
	body, err := json.Marshal(params.Request)
	if err != nil {
		return nil, fmt.Errorf("failed to encode the request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+params.Addr+params.Path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// The server tells the protocols apart by this header. Without it a JSON post is taken
	// for a REST call and looked for among the REST routes, which these RPC paths are not.
	req.Header.Set("Connect-Protocol-Version", "1")

	// An agent running this CLI is named on the call, so its work is logged as its own
	// rather than as an anonymous caller.
	params.From.SetHeader(req.Header)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach the server at %s: %w", params.Addr, err)
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read the response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, readRefusal(payload, resp.Status)
	}

	return payload, nil
}

// readRefusal turns a refusal into the message the service wrote, falling back to the
// status when the body is not the error shape Connect sends.
func readRefusal(payload []byte, status string) error {
	var refusal struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal(payload, &refusal); err == nil && refusal.Message != "" {
		if refusal.Code != "" {
			return fmt.Errorf("%s: %s", refusal.Code, refusal.Message)
		}

		return errors.New(refusal.Message)
	}

	if trimmed := strings.TrimSpace(string(payload)); trimmed != "" {
		return fmt.Errorf("%s: %s", status, trimmed)
	}

	return errors.New(status)
}

// writeResponse prints what the service answered, indented unless a one-line response was
// asked for.
func writeResponse(out io.Writer, payload []byte, compact bool) error {
	rendered := bytes.TrimSpace(payload)

	if !compact {
		var indented bytes.Buffer
		if err := json.Indent(&indented, rendered, "", "  "); err == nil {
			rendered = indented.Bytes()
		}
	}

	if _, err := fmt.Fprintln(out, string(rendered)); err != nil {
		return fmt.Errorf("failed to write the response: %w", err)
	}

	return nil
}
