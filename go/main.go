// Command claude-agents is the tmux panel and CLI for background Claude agents.
package main

import (
	"fmt"
	"os"
	"strings"

	"claude-agents/config"
	"claude-agents/internal/agents"
	"claude-agents/internal/panel"
	"claude-agents/internal/registry"
	"claude-agents/internal/tmux"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	manager := agents.NewAgentManager(agents.Params{
		Tmux:     tmux.NewClient(cfg.TmuxSocket),
		Store:    registry.NewStore(cfg.AgentsDir),
		SelfPath: selfPath(),
	})

	command, rest := parseArgs(os.Args[1:])
	switch command {
	case "notify":
		manager.Notify(cfg.MainSession, cfg.TmuxPane, strings.Join(rest, " "))
	case "status":
		fmt.Print(manager.Status())
	case "hook":
		if len(rest) > 0 {
			manager.Hook(rest[0], cfg.MainSession, cfg.TmuxPane)
		}
	case "chat":
		if len(rest) >= 2 {
			return manager.AttachChat(rest[0], rest[1])
		}
	case "chat-open":
		if len(rest) >= 2 {
			manager.OpenChatPopup(rest[0], rest[1])
		}
	case "panel":
		return panel.Run(manager, firstOrEmpty(rest))
	default:
		// Backward compatible: `claude-agents <client>` opens the panel.
		return panel.Run(manager, command)
	}

	return nil
}

// parseArgs splits argv into a command and its remaining arguments, defaulting
// to the panel when none is given.
func parseArgs(args []string) (string, []string) {
	if len(args) == 0 {
		return "panel", nil
	}

	return args[0], args[1:]
}

func firstOrEmpty(args []string) string {
	if len(args) == 0 {
		return ""
	}

	return args[0]
}

func selfPath() string {
	path, err := os.Executable()
	if err != nil {
		return "claude-agents"
	}

	return path
}
