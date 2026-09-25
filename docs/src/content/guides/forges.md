---
title: Forges
description: How komrade talks to GitHub or GitLab, and how agents open pull requests.
group: operations
order: 8
---

Your `fix-login` [agent](../agents/) has pushed a branch and opened a pull request, and the panel does not know. A forge is where your code is hosted, such as GitHub or GitLab, and komrade connects to it for two reasons. It wants to show you, beside each agent, the pull request that agent's branch has open. And agents need to push and open pull requests without you doing it for them.

## Connecting a forge

You connect a forge once, by handing komrade a token. Komrade checks it with the forge straight away and refuses one that is wrong. Give the token access to the pull requests of the repositories your agents work in. GitHub and GitLab are both supported, including self-hosted. `komrade forge` and `komrade doctor` show what is connected and whether it still works.

## What komrade reads

While an agent is live, komrade watches the pull requests in its repository. It notices when one is opened from the agent's branch, when checks pass or fail, when it is approved, and when it is merged. That shows up as a symbol on the agent's row.

<figure class="placeholder">
  <figcaption>The agents list, with rows showing an open, an approved, a failing and a merged pull request.</figcaption>
</figure>

Komrade asks the forge only what changed since it last looked, and backs off when the forge says it is being asked too often.

## Where the token lives

The database holds only which forge and which account. The token itself sits in a file only your user can read, and never shows up in logs. This is the same arrangement as the GitHub CLI or your SSH keys.

## How agents push

Agents never hold a credential of their own. When an agent's git needs to authenticate, komrade hands it a short-lived token. No token is ever typed into a pane, so none can leak into a transcript.

On your laptop, agents use whatever git and GitHub logins you already have. On our remote engine, komrade uses a GitHub App, which makes tokens that expire after an hour. [Administering the engine](../engine-admin/) sets that up.

Everything so far has run on your laptop, which sleeps. [Running komrade remotely](../remote/) moves the agents somewhere that does not.
