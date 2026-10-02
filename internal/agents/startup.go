package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"komrad/internal/logging"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
)

const startupPolls = 20

const startupPollInterval = 250 * time.Millisecond

const startupTailLines = 5

func (s *LaunchService) watchStartup(ctx context.Context, launched agentstore.Agent) {
	bg := context.WithoutCancel(ctx)

	go func() {
		for range startupPolls {
			<-s.wait(startupPollInterval)

			if s.tmux.HasCLIExited(launched.Pane) {
				s.failExitedStartup(bg, launched)

				return
			}
		}
	}()
}

func (s *LaunchService) failExitedStartup(ctx context.Context, launched agentstore.Agent) {
	agent, err := s.agents.GetByID(ctx, launched.ID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to read an agent whose CLI exited while starting",
				append([]slog.Attr{slog.String(logging.AgentID, launched.ID)}, logging.Err(err)...)...)
		}

		return
	}
	if agent.ArchivedAt != nil || agent.Pane != launched.Pane {
		return
	}

	screen := getLastLines(ansi.Strip(s.tmux.CapturePane(agent.Pane)), startupTailLines)
	s.failCreate(ctx, *agent, fmt.Errorf("the CLI exited while starting: %s", screen))
	s.tmux.KillPane(agent.Pane)
}

func getLastLines(screen string, count int) string {
	var lines []string
	for line := range strings.Lines(screen) {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}

	return strings.Join(lines[max(len(lines)-count, 0):], "\n")
}
