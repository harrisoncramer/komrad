<p align="center">
  <img src="docs/public/favicon.png" alt="komrad" width="120" />
</p>

# komrad

Komrad is a terminal-based agent manager.

It helps you run Claude and Codex agents in a durable, searchable, and coordinated way.

You can use komrad to run production investigations on a schedule, search through old sessions easily, or orchestrate developer workflows via MCP. It's designed with terminal power users in mind, who want to orchestrate agents on their own laptops, without having to run a heavy Electron application.

## Why komrad

Running one agent is easy. Running ten is not, because sessions pile up across terminal panes, a finished conversation is hard to find again, and there is no easy way for one agent to hand work to another.

Komrad gives you one place to browse, search and talk to all of them.

Every agent is a real terminal in a tmux server of komrad's own. Taking over from an agent means attaching to its pane, and any CLI that runs in a terminal works. Closing the panel, or the terminal, stops nothing.

Komrad is unopinionated. It gives you [workflows](https://komrad.dev/latest/guides/workflows/), [agents](https://komrad.dev/latest/guides/agents/), [hooks](https://komrad.dev/latest/guides/hooks/), [schedules](https://komrad.dev/latest/guides/schedules/) and [mail](https://komrad.dev/latest/guides/mail/), and leaves how to combine them up to you.

While komrad doesn't require an account or hosted service, you can run and deploy it remotely to enable "devbox" style development for hands-free or automated flows that don't have any vendor lock-in. Typically, komrad runs on your laptop directly.

Read more in komrad's [docs](https://komrad.dev/latest/).

## Usage

There is no rug-pull. Komrad is free and open source, and is built by [me](https://harrisoncramer.me/about/). If you like it, consider sponsoring me on Github.
