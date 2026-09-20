# Forges

A forge is weave's connection to an external git platform, like GitHub or Gitlab. Weave reads pull requests through a connection to each forge, made once with a token and kept until you disconnect it.

Weave uses this connection to ask the forge questions, such as which pull requests are open on an agent's branch, and to open pull requests on behalf of the user.

## Connecting

Make a personal access token on the forge, then pipe it in:

```bash
gh auth token | weave forge connect github.com
```

Any command that prints the token works there, such as `pbpaste` or `op read`, and `--token-file` reads it from a file instead. The token is never taken as an argument, because arguments are visible to every process on the machine through `ps` and land in shell history.

Before storing anything, weave sends one request to the forge asking who the token belongs to. A token that is wrong or expired is refused at that moment. That proves the token is real, not that it can read your repositories: a fine-grained token with no permissions answers that question too, and shows up later as a repository the forge says it cannot find. Give the token read access to pull requests on the repositories your agents work in.

To see your current forges:

```bash
weave forge list
```

This shows every connected forge, who weave acts as there, and when the forge last confirmed it. `weave forge check` asks every connected forge again and records the answer, which is what `weave doctor` runs. You can use `weave forge disconnect github.com` to forget a forge.

Every one of these commands is a client of the running service, which is what holds the connections. Add `--engine <name>` to run one against an engine instead of the laptop.

`github.com` and `gitlab.com` say which forge they run. Any other host needs `--kind`, and weave then looks for its API at `https://<host>/api/v3` for GitHub or `/api/v4` for GitLab, which `--api-url` overrides. The address has to be https, since the token travels in every request to it.

## Reading pull requests

```bash
weave pr list git@github.com:owner/repo.git
weave pr list git@github.com:owner/repo.git some-branch
```

Give the repository the way a git remote spells it, since the host is what picks the connection. Without a branch, this lists the repository's open pull requests. With one, it lists every pull request opened from that branch whatever its state, which is the question the pull request sweep will ask.

The forge answers a hundred at a time, so the endpoint pages: `list_pull_requests` takes a limit of at most a hundred, twenty when unset, and answers a `next_page_token` to pass back as the cursor. The command follows every page itself.

A repository the credential cannot see answers not found, the same as one that does not exist, since the forge does not tell the two apart.

The service also reads pull requests on its own, once a minute, for every repository a live agent sits on: what changed since the last pass, the branches agents sit on that it has not listed yet, and for each open pull request on one of those branches that changed, where the forge says its reviews stand. That is what fills the pull request glyph on each agent's row and the `pull request` rows in the details, described in [agents](agents.md#pull-requests). A pass makes at most ten requests per repository, and a repository that has not changed costs one that the forge does not count. GitHub and GitLab are both read, a GitLab merge request being a pull request to weave.

## Where authentication state lives

The database holds non-sensitive information: the host, which forge it runs, the login the forge answered with, and when that was last confirmed.

The token itself is kept on disk at `~/.local/state/weave/forge/<connection id>`, which is readable only by your user. Every request weave makes to the forge reads that file, puts the token in the request's header, and forgets it. Nothing keeps a copy in memory between calls; a restart changes nothing.

Inside weave the token is a type that prints masked, so that log lines and errors obfuscate it. This is enforced via a linter.

## Local Security Posture

For a personal laptop, weave's default security model is identical to tools like the GitHub CLI, which also keeps the same token in `~/.config/gh/hosts.yml` for instance, or SSH keys that live in the `~/.ssh` directory.

However, personal tokens do not expire by default with Weave. Weave will notice an expired token, with a 401, and both `weave forge list` and `weave doctor` say so.

## Remote (Engine) Security Posture

When running weave remotely, I recommend using a GitHub App identity to mint a token which dies in an hour, described in [administering the engine](engine-admin.md). With the App configured, `weave forge connect github.com --method github-app` connects the engine's forge through it. No token is stored under `forge/`: the App mints one that dies an hour later, and the service keeps it in `github-token.json` under its cache directory, readable only by the weave user, until then. The login recorded is the App's own name.
