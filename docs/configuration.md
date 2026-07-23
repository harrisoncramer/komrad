# Configuration

There are two layers: environment variables that set paths and addresses, and a TOML file that sets defaults and lifecycle hooks for new sessions.

## Environment

The environment configuration is loaded via ultra in the `config` package. Do not call `os.Getenv` directly anywhere in the code; the custom linter forbids it. See [development](development.md).

- `CLAUDE_AGENTS_DB` is the database path. It defaults to `~/.claude/claude-agents.db`.
- `CLAUDE_AGENTS_ADDR` is the session server listen address. It defaults to `127.0.0.1:8787`.
- `CLAUDE_AGENTS_TMUX_SOCKET` is the tmux socket. It defaults to `default`.
- `MAIN_SESSION` and `TMUX_PANE` identify the calling agent. The popup exports `MAIN_SESSION`, and `TMUX_PANE` is always set inside tmux.

## App config

The app config is a TOML file loaded via `internal/appconfig`. It lives at `~/.config/claude-agents/config.toml`, or wherever `--config <path>` points. Copy `config.example.toml` to start.

The `claude_flags` field is a string of default flags appended to `claude` for every new session. The new-chat form can add more on top.

The `[hooks]` table names paths to lifecycle scripts, each an executable with `~` expanded.

- `pre_create` runs before launch with the session name and the requested directory. Whatever it prints on stdout becomes the directory Claude runs in. This is how a new session can spin up a git worktree and launch Claude inside it, for example by calling a worktrunk script and echoing the new tree's path.
- `post_create` runs after launch with the session name and directory.
- `on_delete` runs on destroy with the session name.

The new-chat form's `Directory` field is what the `pre_create` hook receives as the requested directory, alongside the session name.

## Keybindings

The `[keybindings]` table remaps the panel's keys, scoped to the panel each binding acts in. Each action takes a list of key strings, and any action left out keeps its default. Key names are bubbletea key strings such as `enter`, `esc`, `ctrl+d`, `up`, and `k`.
