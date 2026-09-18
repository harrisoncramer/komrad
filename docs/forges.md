# Forges

A forge is the service a repository's pull requests live on, such as GitHub. Weave reads pull requests through a connection to each forge, made once with a token and kept until you disconnect it. Nothing in weave runs `gh` or `glab` to do this; the token is what weave holds, and it speaks to the forge's API directly.

Connecting is separate from pushing. An agent pushes with whatever git credentials the machine already has, the same as before. A forge connection is only for weave itself to ask the forge questions, such as which pull requests are open on an agent's branch.

## Connecting

Make a personal access token on the forge, then pipe it in:

```bash
gh auth token | weave forge connect github.com
```

Any command that prints the token works there, such as `pbpaste` or `op read`, and `--token-file` reads it from a file instead. The token is never taken as an argument, because arguments are visible to every process on the machine through `ps` and land in shell history.

Before storing anything, weave sends one request to the forge asking who the token belongs to. A token that is wrong, expired or scoped to nothing is refused at that moment, where you can fix it, rather than found out later by a background job.

```bash
weave forge list
```

shows every connected forge, who weave acts as there, and when the forge last confirmed it. `weave forge disconnect github.com` forgets a forge and the token it was connected with. `weave doctor` asks every connected forge again and warns for any that refuses.

`github.com` and `gitlab.com` say which forge they run. Any other host needs `--kind`, and `--api-url` when the API is not at the host's usual place.

## Where the token lives

Two places, and only one holds the secret.

The database row holds the host, which forge it runs, the login the forge answered with, and when that was last confirmed. It holds no token, so `weave forge list` through the panel's own SQL tool shows everything but the secret.

The token itself is one file, `~/.local/state/weave/forge/<connection id>`, readable only by your user. Every request weave makes to the forge reads that file, puts the token in the request's header, and forgets it. Nothing keeps a copy in memory between calls, so a restart changes nothing.

Inside weave the token is a type that prints masked. A log line or an error that carries one shows its last three characters, `*****fba`, which is enough to tell two tokens apart and nothing an attacker can use. The one method that gives the token back may only be called in the code that sends it and the code that stores it, and lint refuses it anywhere else.

## Is a file on disk safe enough

For a laptop, it is the same protection every developer tool on the machine already relies on. The GitHub CLI keeps the same token in `~/.config/gh/hosts.yml`. Git's credential store keeps one in `~/.git-credentials`. SSH keys sit in `~/.ssh`. All of them are files readable by your user, and the token weave holds reaches the same repositories those do. Nobody who can read weave's file was stopped from reading the others.

What a file does not protect against is another process running as your user, which can read it without asking. That is true of the files above too. The thing that would prevent it is the operating system's keychain, which prompts before handing a secret to a process. Weave keeps the token behind an interface so the keychain can be a second place to keep it, but that is not built yet, and a remote engine has no keychain in any case.

The token is not encrypted in the database, on purpose. Encrypting it would need a key, and the key would have to live on the same disk as the file it protects, which is the same problem with an extra step.

## When a token stops working

A personal token does not expire unless you told the forge to make it, and the forge does not say when one will. So weave does not track expiry. What it does is notice refusal: the first request the forge answers with a 401 marks the connection as refused, `weave forge list` and `weave doctor` say so, and the fix is to disconnect and connect again with a new token. There is no automatic recovery, because there is nothing to recover from without a token from you.

## On a remote engine

None of the above applies to an engine yet. An engine has nobody to paste a token and no keychain, and it already holds a GitHub App identity that mints a token which dies in an hour, described in [administering the engine](engine-admin.md). Connecting an engine's forge through that App is the next piece of this work.
