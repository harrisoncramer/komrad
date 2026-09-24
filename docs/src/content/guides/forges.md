---
title: Forges
description: Connect weave to GitHub or GitLab so it can read and open pull requests.
group: operations
order: 9
---

A forge is a git hosting service such as GitHub or GitLab. Weave connects to each forge once, with a token, and keeps the connection until you remove it. It uses the connection to see which pull requests are open on each agent's branch, and so agents can open pull requests.

## Connecting

Make a personal access token on the forge and pipe it in.

```sh
gh auth token | weave forge connect github.com
```

Anything that prints the token works, such as `pbpaste` or `op read`, and `--token-file` reads it from a file. Weave never takes the token as an argument, because arguments are visible to every process through `ps` and end up in shell history.

Before storing the token, weave asks the forge who it belongs to, so a wrong or expired token is refused straight away. That only proves the token is real. A token with no permissions passes this check and later can't see your repositories, so give it read access to pull requests on the repositories your agents work in.

`weave forge list` shows every connected forge, who weave acts as there, and when the forge last confirmed it. `weave forge check` asks every forge again, which is what `weave doctor` runs. `weave forge disconnect github.com` removes a forge. These commands talk to the running service, and `--engine <name>` runs one against a remote engine instead.

`github.com` and `gitlab.com` are recognised on their own. For any other host, pass `--kind`. Weave then looks for the API at `https://<host>/api/v3` for GitHub or `/api/v4` for GitLab, and `--api-url` overrides that. The address has to be https, because the token is sent with every request.

## Reading pull requests

```sh
weave pr list git@github.com:owner/repo.git
weave pr list git@github.com:owner/repo.git some-branch
```

Give the repository the way a git remote spells it, because the host decides which connection is used. Without a branch this lists the open pull requests. With a branch it lists every pull request opened from it, in any state.

A repository the token cannot see looks exactly like one that does not exist, because the forge does not tell the two apart.

The service also reads pull requests itself, once a minute, for every repository a live agent is on. That fills in the pull request symbol on each agent's row, as described in [agents](../agents/#pull-requests). Each pass is kept small. A new repository is read back thirty days. After that, one search per forge host each minute finds only the pull requests that changed. Both GitHub and GitLab are read, and a GitLab merge request counts as a pull request.

## Where the token lives

The database holds only what is not secret: the host, the kind of forge, the login it answered with, and when that was last confirmed.

The token is kept in `~/.local/state/weave/forge/<connection id>`, which only your user can read. Each request reads the file, puts the token in the request header, and forgets it. Logs and errors show the token masked.

On a laptop this is the same arrangement as the GitHub CLI keeping its token in `~/.config/gh/hosts.yml`, or your SSH keys in `~/.ssh`. Personal tokens do not expire unless you set them to. Weave notices when one has expired, and both `weave forge list` and `weave doctor` report it.

## How agents push and open pull requests

Agents never hold a credential of their own.

Git asks a credential helper for a password each time it needs one. On a machine set up as in [administering the engine](../engine-admin/), that helper is `weave github credential`, which hands git a token that expires within the hour.

The `gh` CLI cannot ask a helper, because it only reads `GH_TOKEN` when it starts. So weave puts a small stand-in for `gh` ahead of the real one on every agent's `PATH`. The stand-in asks weave for a fresh token, sets it for that one `gh` process, and runs the real binary. Nothing is exported into the pane, so no token appears in a screen capture or a transcript.

On a machine with no `[github_app]` configured, the stand-in does nothing and runs the real `gh`, which uses whatever login the machine has. A `GH_TOKEN` or `GITHUB_TOKEN` already set is left alone too.

When weave does hold the credential and cannot help, it refuses clearly. If the checkout's origin is on a forge the App cannot reach, or the App cannot make a token, the agent gets weave's own error and a non-zero exit. On a machine with no `gh` installed, the stand-in says so and exits 127.

## On a remote engine

A remote engine should use a GitHub App, which makes tokens that expire after an hour. [Administering the engine](../engine-admin/) sets one up. With the App configured, `weave forge connect github.com --method github-app` connects the engine's forge through it. No token is stored under `forge/`. The service keeps the App's current token in `github-token.json` in its cache directory, readable only by the weave user, until it expires. The login recorded is the App's own name.
