package cli

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	sessionv1 "claude-agents/proto/gen/session/v1"
	"claude-agents/proto/gen/session/v1/sessionv1connect"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
)

// newCmd creates a chat from the command line, through the service like every
// other caller.
//
// It goes over the wire rather than driving the store directly because a chat
// created with nothing serving is only half a chat: no reconciler moves its
// state, and no syncer makes it searchable. Requiring the server is honest about
// a dependency that was always there, and it puts this call behind the same
// validation the panel and the tools pass through.
func newCmd(state *app) *cobra.Command {
	var (
		title  string
		dir    string
		flags  string
		prompt string
		client string
	)

	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a background agent session non-interactively",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := ensureServer(state.env.Addr, selfPath()); err != nil {
				return err
			}

			sessions := sessionv1connect.NewSessionServiceClient(http.DefaultClient, "http://"+state.env.Addr)

			created, err := sessions.CreateSession(cmd.Context(), connect.NewRequest(&sessionv1.CreateSessionRequest{
				Prompt: prompt,
				Title:  title,
				Dir:    state.resolveDir(dir, client),
				Flags:  flags,
			}))
			if err != nil {
				return fmt.Errorf("failed to create session: %w", err)
			}

			cmd.Println(created.Msg.GetSession().GetName())

			return nil
		},
	}

	cmdFlags := cmd.Flags()
	cmdFlags.StringVar(&title, "title", "", "session title (blank auto-names)")
	cmdFlags.StringVar(&dir, "dir", "", "working directory (blank = current)")
	cmdFlags.StringVar(&flags, "flags", "", "extra claude flags")
	cmdFlags.StringVar(&prompt, "prompt", "", "initial prompt")
	cmdFlags.StringVar(&client, "client", "", "invoking tmux client")

	return cmd
}

// resolveDir answers where a new chat should start when the caller left it
// blank. The service refuses a blank directory because it cannot know which pane
// the request came from; this side can, so it asks tmux and falls back to home.
func (a *app) resolveDir(dir, client string) string {
	if trimmed := strings.TrimSpace(dir); trimmed != "" {
		return trimmed
	}
	if path := a.tmux.ClientPath(client); path != "" {
		return path
	}

	home, _ := os.UserHomeDir()

	return home
}
