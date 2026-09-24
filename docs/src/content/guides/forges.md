---
title: Forges
description: How komrade talks to GitHub or GitLab, and how agents open pull requests.
group: operations
order: 9
---

A forge is where your code is hosted, such as GitHub or GitLab. Komrade connects to it for two reasons. It wants to show you, beside each agent, the pull request that agent's branch has open and where it stands. And agents need to push branches and open pull requests without you doing it for them.

## Connecting a forge

You connect a forge once, by handing komrade a token. Komrade checks the token with the forge straight away and refuses one that is wrong or expired. That check only proves the token is real. It does not prove the token can see your repositories, so give it access to the pull requests of the repositories your agents work in. GitHub and GitLab are both supported, including self-hosted installs.

Komrade keeps a connection until you remove it, and tells you when a token has stopped working. The `komrade forge` and `komrade doctor` commands show what is connected and whether it still works, and the [CLI reference](../../cli/forge/) has the details.

## What komrade reads

While an agent is live, komrade keeps an eye on the pull requests in its repository. It notices when one is opened from the agent's branch, when checks pass or fail, when reviewers approve or ask for changes, when it conflicts with its base, and when it is merged or closed. That shows up as a small symbol on the agent's row, and in more detail in the agent's record.

<figure class="placeholder">
  <figcaption>The agents list, with rows showing an open, an approved, a failing and a merged pull request.</figcaption>
</figure>

Komrade does this quietly and cheaply. It asks the forge only what has changed since it last looked, and backs off when the forge says it is being asked too often. A GitLab merge request counts as a pull request.

## Where the token lives

The database only holds what is safe to store: which forge, which account the token belongs to, and when that was last checked. The token itself sits in a file that only your user can read, and komrade reads it for each request rather than keeping it in memory. It never shows up in logs.

On a laptop this is the same arrangement as the GitHub CLI or your SSH keys. A personal token does not expire unless you set one to, which is why komrade watches for tokens that have.

## How agents push and open pull requests

Agents never hold a credential of their own. When an agent's git needs to authenticate, it asks komrade, and komrade hands over a short-lived token. When an agent runs the GitHub CLI, komrade quietly supplies a fresh token to that one command and nothing else. No token is ever typed into an agent's pane, so none can leak into a transcript.

On your own laptop, komrade stays out of the way and agents use whatever git and GitHub logins you already have. On a remote engine, komrade uses a GitHub App, which makes tokens that expire after an hour and only reach the repositories you chose. [Administering the engine](../engine-admin/) sets that up.

When komrade is responsible for the credential and cannot provide one, it says so clearly. The agent learns it cannot open the pull request, instead of reading a confusing error from an unauthenticated tool.
