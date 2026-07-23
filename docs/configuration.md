# Configuration

There are two layers: environment variables that set paths and addresses, and a TOML file that sets defaults for new sessions.

## Environment

The environment configuration is loaded via ultra in the `config` package. Do not call `os.Getenv` directly anywhere in the code; the custom linter forbids it. See [development](development.md).

- `CLAUDE_AGENTS_DB` is the database path. It defaults to `~/.claude/claude-agents.db`.
- `CLAUDE_AGENTS_ADDR` is the session server listen address. It defaults to `127.0.0.1:8787`.
- `CLAUDE_AGENTS_TMUX_SOCKET` is the tmux socket. It defaults to `default`.
- `MAIN_SESSION` and `TMUX_PANE` identify the calling agent. The popup exports `MAIN_SESSION`, and `TMUX_PANE` is always set inside tmux.

## App config

The app config is a TOML file loaded via `internal/appconfig`. It lives at `~/.config/claude-agents/config.toml`, or wherever `--config <path>` points. The repo's `config.toml` lists every setting at its default value; copy it there to start.

The `claude_flags` field is a string of default flags appended to `claude` for every new session. The new-chat form can add more on top.

## Keybindings

The `[keybindings]` table remaps the panel's keys, scoped to the panel each binding acts in. Each action takes a list of key strings, and any action left out keeps its default. Key names are bubbletea key strings such as `enter`, `esc`, `ctrl+d`, `up`, and `k`.
