<p align="center">
  <img src="docs/public/favicon.png" alt="komrade" width="120" />
</p>

# komrade

Agents of the world, unite!

Komrade is a terminal-based agent manager. It helps you runs Claude and Codex agents, keeps track of them, and lets them work together.

You can use komrade to manage, restore, and search through old agent sessions, launch and manage them with komrade's MCP, schedule agents, and much more. It's designed to be familiar to terminal power users.

It's free and open source, and is built by [me](https://harrisoncramer.me/about/) so there'll be no rug-pull.

## Why komrade

Running one agent is easy. Running ten is not, because sessions pile up across terminal panes, a finished conversation is hard to find again, and there is no easy way for one agent to hand work to another.

Komrade gives you one place to browse, search and talk to all of them.

Every agent is a real terminal in a tmux server of komrade's own. Taking over from an agent means attaching to its pane, and any CLI that runs in a terminal works. Closing the panel, or the terminal, stops nothing.

Komrade is unopinionated. It gives you [workflows](https://komrade.dev/latest/guides/workflows/), [agents](https://komrade.dev/latest/guides/agents/), [hooks](https://komrade.dev/latest/guides/hooks/), [schedules](https://komrade.dev/latest/guides/schedules/) and [mail](https://komrade.dev/latest/guides/mail/), and leaves how to combine them up to you.

While komrade doesn't require an account or hosted service, you can run and deploy it remotely to enable "devbox" style development for hands-free or automated flows that don't have any vendor lock-in. Typically, komrade runs on your laptop directly.

Read more in komrade's [docs](https://komrade.dev/latest/).
