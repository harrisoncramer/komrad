package cli

import (
	"context"
	"fmt"
	"strings"

	"claude-agents/internal/store"

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

// statusCmd prints the global agent summary for the tmux status bar. The
// rendering lives here rather than in the session layer because tmux's colour
// markup is presentation, and nothing else needs it.
func statusCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print the status-bar aggregate",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Print(renderStatus(cmd.Context(), state.sessions))
			return nil
		},
	}
}

// statusLister is the part of the store the summary reads. A failed listing
// renders as nothing at all, since a status bar is not the place to report an
// error the user cannot act on.
type statusLister interface {
	List(ctx context.Context) ([]store.Agent, error)
}

func renderStatus(ctx context.Context, sessions statusLister) string {
	agents, err := sessions.List(ctx)
	if err != nil {
		return ""
	}

	waiting, working, done := 0, 0, 0
	for _, agent := range agents {
		switch agent.State {
		case store.StateThinking:
			working++
		case store.StateWaiting:
			waiting++
		case store.StateDone:
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
