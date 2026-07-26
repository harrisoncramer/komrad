package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"claude-agents/internal/client"
	sessionv1 "claude-agents/proto/gen/session/v1"
	"claude-agents/proto/gen/session/v1/sessionv1connect"

	"claude-agents/internal/store/session"

	"github.com/spf13/cobra"
)

// Status-bar colours, matching the panel's own palette so the summary reads as
// part of the same tool.
const (
	statusBase    = "#[fg=#7E9CD8]"
	statusWaiting = "#[fg=#E82424]"
	statusWorking = "#[fg=#FF9E3B]"
	statusDone    = "#[fg=#98BB6C]"
)

// statusTimeout bounds the read behind one status-bar refresh. tmux redraws the
// bar on a timer, so a slow or missing server has to cost a frame rather than
// hold the bar up.
const statusTimeout = 2 * time.Second

// statusCmd prints the global agent summary for the tmux status bar. It reads
// through the service like everything else, and renders here because tmux's
// colour markup is presentation that nothing else needs.
//
// It does not start the server. A status bar that spawned a daemon on a redraw
// would be a surprise, and with nothing serving there is nothing to report: no
// reconciler is moving any chat's state, so the honest summary is an empty one.
func statusCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print the status-bar aggregate",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), statusTimeout)
			defer cancel()

			sessions := sessionv1connect.NewSessionServiceClient(http.DefaultClient, "http://"+state.env.Addr)

			listed, err := client.ListAllSessions(ctx, sessions)
			if err != nil {
				// Nothing to say, and nowhere useful to say it.
				return nil
			}

			fmt.Print(renderStatus(listed))

			return nil
		},
	}
}

// renderStatus counts the chats worth reporting and renders them as tmux markup.
func renderStatus(sessions []*sessionv1.Session) string {
	waiting, working, done := 0, 0, 0
	for _, chat := range sessions {
		switch chat.GetState() {
		case session.StateThinking:
			working++
		case session.StateWaiting:
			waiting++
		case session.StateDone:
			done++
		}
	}

	var segments []string
	if waiting > 0 {
		segments = append(segments, fmt.Sprintf("%s%d waiting%s", statusWaiting, waiting, statusBase))
	}
	if working > 0 {
		segments = append(segments, fmt.Sprintf("%s%d working%s", statusWorking, working, statusBase))
	}
	if done > 0 {
		segments = append(segments, fmt.Sprintf("%s%d done%s", statusDone, done, statusBase))
	}
	if len(segments) == 0 {
		return ""
	}

	return strings.Join(segments, "  ") + "  "
}
