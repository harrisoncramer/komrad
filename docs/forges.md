# Forges

A forge is weave's connection to an external git platform, like GitHub or Gitlab. Weave reads pull requests through a connection to each forge, made once with a token and kept until you disconnect it.

Weave uses this connection to ask the forge questions, such as which pull requests are open on an agent's branch, and to open pull requests on behalf of the user.

## Connecting

Make a personal access token on the forge, then pipe it in:

```bash
gh auth token | weave forge connect github.com
```

Any command that prints the token works there, such as `pbpaste` or `op read`, and `--token-file` reads it from a file instead. The token is never taken as an argument, because arguments are visible to every process on the machine through `ps` and land in shell history.

Before storing anything, weave sends one request to the forge asking who the token belongs to. A token that is wrong, expired or scoped to nothing is refused at that moment.

To see your current forges:

```bash
weave forge list
```

This shows every connected forge, who weave acts as there, and when the forge last confirmed it. You can use `weave forge disconnect github.com` to forget a forge, and `weave doctor` asks every connected forge again and warns for any that refuses.

## Where authentication state lives

The database holds non-sensitive information: the host, which forge it runs, the login the forge answered with, and when that was last confirmed.

The token itself is kept on disk at `~/.local/state/weave/forge/<connection id>`, which is readable only by your user. Every request weave makes to the forge reads that file, puts the token in the request's header, and forgets it. Nothing keeps a copy in memory between calls; a restart changes nothing.

Inside weave the token is a type that prints masked, so that log lines and errors obfuscate it. This is enforced via a linter.

## Local Security Posture

For a personal laptop, weave's default security model is identical to tools like the GitHub CLI, which also keeps the same token in `~/.config/gh/hosts.yml` for instance, or SSH keys that live in the `~/.ssh` directory.

However, personal tokens do not expire by default with Weave. Weave will notice an expired token, with a 401, and both `weave forge list` and `weave doctor` say so.

## Remote (Engine) Security Posture

When running weave remotely, I recommend using a GitHub App identity to mint a token which dies in an hour, described in [administering the engine](engine-admin.md). Connecting an engine's forge through that App is the next piece of this work.
