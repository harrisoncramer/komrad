# A stable release channel

## Summary

Add a second channel alongside the rolling one. Stable is a semver tag on main, pushed deliberately by a person, starting at `v0.x`. A tag push builds the same four binaries and cuts a real GitHub release with a changelog, and the remote engine is deployed from that release rather than from every merge. The machine image stops riding on merges too and becomes a workflow somebody presses. Main stays always-releasable and there is no second branch.

This reverses RFC 0018's position that every merge is the release. That RFC deliberately chose a rolling prerelease over a tagging policy, on the grounds that getting a binary onto a machine was one workflow and one mise task, while the tagged plan was gated on a module rename, a versioning commitment, a tap repository and a token, none of which had to happen first. It was right, it shipped, and nothing here touches it. What changes is that `tip` stops being the only channel, and that shipping stops being something a merge does on your behalf.

## Problem

Merging and shipping are the same act today, in three places, on three different clocks that have all been collapsed into one.

`main-release.yml` republishes `tip` on every merge. That is correct and should stay: `tip` is the tip of main by definition, and its release notes say so in as many words.

`engine.yml` deploys the remote engine on every merge. That one is not correct. The engine is a shared machine running everybody's agents, and `remote:deploy` ends in `systemctl restart weave.service`. An intermediate commit that passes CI and breaks the engine at runtime costs every agent on the box, and nothing about merging a pull request says the author intended that. The same workflow also builds a machine image on every merge that touches `packer/`, which is cheap and harmless, but it is a third clock bolted to the same trigger.

`engine.yml` also declares:

```yaml
concurrency:
  group: engine
  cancel-in-progress: false
```

So four merges landing in ten minutes queue four full deploys, each one a CI run, a cross-compile, an S3 round trip and a service restart, of which only the last one's output survives. That queue is the symptom that started this.

And there is nothing to install if you do not want the tip of main. `docs/install.md` offers one URL, `releases/download/tip/weave-darwin-arm64`, and describes upgrading as downloading over the old binary. There is no version to ask for, no changelog to read, and no way to stay put. `git tag` in this repository returns `iss-479-backup`, `rfc-0017-identifiers` and `tip`. No release has ever been cut.

## Proposal

### One branch, and a tag

Stable is a tag. There is no `stable` branch and no release branch, because a second branch means backports, and a project with one maintainer and no users should not own a backport policy. Main stays always-releasable, which is a convention held up by `ci.yml` rather than by anything that can be enforced here yet, for reasons in the public-repository section below.

Tags are `vMAJOR.MINOR.PATCH`, annotated, starting at `v0.1.0`. The `v` prefix matches what `internal/version` already carries everywhere and what the existing stamps look like. Staying on `v0.x` is not modesty, it is accuracy: `AGENTS.md` says weave keeps no backward compatibility, a migration may be rewritten and a config key renamed outright, so every release is entitled to break something and `0.x` is the semver arm that says so. A minor bump is the normal release. A patch bump is for a fix on top of one.

### Cutting a release

A mise task, `mise run release:cut v0.3.0`, next to the other tasks rather than a second place to name a version. It checks and then tags:

- the argument parses as `vN.N.N`
- the tree is clean, using the `tree_is_dirty` helper already in `mise/tasks/_build`
- HEAD is on `main` and equal to `origin/main`, so the tag names what CI actually tested
- the tag does not already exist locally or on the remote
- CI on that commit is green, read with `gh run list`

Then `git tag -a` with the version as the message and `git push origin refs/tags/vN.N.N` by full ref, the same spelling `main-release.yml` uses for `tip` and for the same reason.

The push is made with the person's own credentials, which matters more than it looks. A tag pushed by a workflow's `GITHUB_TOKEN` does not trigger other workflows; GitHub suppresses that to stop a workflow looping on itself. So the tag has to be pushed by a human for the release to build at all, and this task is how.

### The release workflow

A new `.github/workflows/release.yml`:

```yaml
on:
  push:
    tags: ['v*']
  workflow_dispatch:
    inputs:
      tag:
        required: true
```

The `workflow_dispatch` is there because a tag push is a one-shot trigger and a release that fails halfway cannot be retried by pushing the tag again, since the tag already exists and force-moving it would be a lie about what was released. Dispatch names the tag as an input, checks it out by that ref, and runs the same jobs. Every step downstream keys off one resolved `TAG` value so the two entry points cannot drift.

Three jobs.

`check` calls `ci.yml` through `workflow_call`, the same move `main-release.yml` makes. Yes, that commit already passed CI on main. Paying for it again is a few minutes of runner time against publishing an artifact that nothing gated, and it is the price already accepted for `tip`. No line of `ci.yml` changes: the reusable trigger is already there and the `changes` job already runs every filter when the event is not a pull request. What it does bring along is its own concurrency policy, which is picked up below.

`publish` builds with `WEAVE_VERSION=$TAG mise run go:release`, which the task already reads and which stamps both the version and the commit through `go_build_binary`. It then cuts a real release, not a prerelease, with `gh release create "$TAG" --title "$TAG" --generate-notes --verify-tag`, and uploads `dist/*`. `--generate-notes` is what makes this a release rather than four files: GitHub writes the changelog from the merged pull requests between this tag and the last, which is exactly the thing `tip` can never have.

`deploy` puts that build on the remote engine, and is the whole point of separating merging from shipping. It is one line calling `engine.yml`, for reasons in the next section, which is also where the concurrency that started all this ends up.

### The engine follows releases

`engine.yml` has four jobs today: `ci`, `changes`, `deploy` and `image`. They are split by the clock each one actually runs on. `image` leaves for `image.yml`, below, and `changes` goes with it, since the paths filter it computes has no other consumer and `image.yml` is dispatch-only and needs no filter at all. What is left is `deploy`, and `engine.yml` keeps it, losing its `push: branches: [main]` trigger and gaining `workflow_call` beside the `workflow_dispatch` it already has. `release.yml`'s third job is then one line, `uses: ./.github/workflows/engine.yml`, needing `publish`.

`ci` is the job the two entry points disagree about, so it is decided here rather than by whoever writes the workflow. A dispatch deploys an arbitrary ref, and the full suite is the only thing standing between a half-finished branch and a `systemctl restart` on the machine running everybody's agents, so the dispatch path keeps it. The release path does not need it: that commit has already passed CI on the merge and again as `release.yml`'s `check`, and a third run in the called workflow is pure waste that would also undo the arithmetic two paragraphs above, where paying for CI twice is weighed and accepted. Twice is the number that argument survives.

So `engine.yml` declares a `run_checks` input on `workflow_call`, defaulting to true, and `release.yml` passes it false. An input rather than a condition on the event, because a called workflow reports the caller's event: `github.event_name` inside `engine.yml` is `push` when a tag triggered `release.yml` and `workflow_dispatch` when somebody re-ran a release, so gating the `ci` job on it would skip the checks on a direct dispatch and run them on a release, which is backwards. The input says what is meant and cannot be read two ways.

That makes `deploy` depend on a job that is sometimes skipped, which by default skips `deploy` with it. The `needs:` line is `needs: ci` with `if: ${{ !failure() && !cancelled() }}`, which runs the deploy when the checks passed or did not run, and not when they failed. It is the same shape as `ci.yml`'s own `required` gate, which exists for the same reason: a skipped job is not a failed one.

Keeping it as a reusable workflow rather than folding it into `release.yml` is what stops something being lost. A `workflow_dispatch` on GitHub takes a ref, so today anybody can deploy any branch to the engine from the Actions tab with no local credentials at all: the runner assumes `vars.AWS_CI_ROLE` by OIDC, writes the backend config from repository variables, and reads Terraform state from the bucket. `mise run remote:deploy` is not a replacement for that. It calls `require_aws_profile`, reads the instance id out of Terraform state in your own checkout, presigns an S3 URL and drives SSM, all of which need working AWS credentials on the machine in front of you. Deploying when the laptop is the thing that is broken, or when its AWS session has expired, is exactly the case the dispatch covers and the local task cannot.

The deploy job itself is unchanged in substance: assume the AWS role, write `infra/terraform/backend.hcl` from the repository variables, `terraform:init -lockfile=readonly`, then `mise run remote:deploy`. One thing is added. `remote:deploy` hardcodes `VERSION="v0.0.0-engine.$REVISION$DIRTY"`, and it should take `WEAVE_VERSION` from the environment first, the way `go:release` does, so a released engine reports `v0.3.0` instead of `v0.0.0-engine.abc1234` and `weave version` against it reads as an answer rather than a fingerprint.

That passthrough needs a rule the version in `go:release` does not, and the rule is that `WEAVE_VERSION` is honoured only on a clean tree and refused otherwise. `remote:deploy` already computes `DIRTY="+dirty.$(date +%H%M%S)"` and folds it into the version, and `build_commit` in `mise/tasks/_build` returns nothing at all from a dirty tree. So a dirty deploy is the one shape where the version string is the only identification anybody has, and letting an exported `WEAVE_VERSION` win there would put a binary on the engine that answers `v0.3.0` and is not `v0.3.0`. The build guard still refuses that pair, since the panel carries a commit and the engine carries none, so this is not a correctness hole. It is the failure named at the end of this document, quietly running an engine nobody can name, arrived at by way of a confident wrong answer. `go:release` gets away with the plain passthrough because CI is its only caller and CI is always clean; `remote:deploy` is a task a person runs from a working tree, and this RFC is promoting that as the escape hatch, so it will be run dirty.

Deploying from `release.yml` rather than from a `release: published` trigger on a second workflow is a choice about credentials. A release created with the workflow's own `GITHUB_TOKEN` raises no event that starts another run, so the two-workflow shape only works if the release is cut with a personal access token or a GitHub App token instead. That is a long-lived `contents: write` credential sitting in the release path, and the deploy job already needs OIDC for AWS, so the second workflow would buy readability at the cost of a second kind of secret. One workflow with an ordinary `needs:` edge is cheaper.

`mise run remote:deploy` stays exactly as it is, run by hand, as the way to put an unreleased build on the box. That is what it is for and this change makes it more useful rather than less, because the engine no longer moves under you between the moment you deploy and the next merge.

The concurrency setting that started this earns its keep, but not for the reason it was written. With merges no longer deploying, the train of redundant deploys cannot form, so `cancel-in-progress: false` is no longer queueing work that is about to be thrown away. What is left is a real mutex over one machine: two releases cut close together, or a dispatch racing a tag push, would otherwise both drive an SSM command that installs a binary and restarts a service on the same instance. Cancelling one of those mid-flight does not stop the command already running on the box, it only stops anybody watching what it returned.

So it stays where it is, at workflow level in `engine.yml`, with the group spelled as the literal `engine` rather than derived from `github.workflow`. That spelling is what makes it work: a called workflow resolves `github.workflow` to its caller, so a group named after it would put the release path and the dispatch path in different groups and the mutex would cover neither. A literal name covers both entry points, which is the whole reason the deploy stays a workflow rather than becoming a job.

Two things it does not cover, both worth saying rather than discovering. `mise run remote:deploy` from a laptop drives the same SSM command against the same instance and is in no GitHub concurrency group at all, so the guarantee is that two CI-driven deploys cannot overlap, not that two deploys cannot overlap. The likeliest collision in practice is Harry hand-deploying his checkout while a release he cut is still running, and nothing here prevents it. And `release.yml` inherits a second, contradictory policy from the `check` job: `ci.yml` declares `cancel-in-progress: ${{ github.ref != 'refs/heads/main' }}`, which at a tag ref is true. That call lands in a group of its own, keyed on the caller and the tag, so a tag push and a dispatch of the same tag cancel each other's CI while their deploys queue. That is the behaviour to want, but the claim that nothing in `ci.yml` changes is true only in the sense that no line of it is edited.

### The image is a third artifact on its own clock

`image.yml`, `workflow_dispatch` only, holding what was `engine.yml`'s `image` job: assume the role, `mise run packer:build`, scrape the AMI id out of the log, write it to SSM, and put it in the step summary.

Building an image and applying one are different acts. Building is cheap, side-effect free, and produces an id. Applying replaces the instance and stops every agent on it, which is why `infra/README.md` says that changing `ami_id` replaces the instance and stops every agent on it, and that this is the upgrade path rather than an accident, and why the current workflow prints the id and stops. That rule survives this change. The workflow builds and records; a person applies, when the box is free, and `mise run remote:reimage` is still what they run.

Losing the `packer/**` paths filter is the deliberate part. Today the image rebuilds when its inputs change, which sounds right and means the image is rebuilt by merges that nobody meant as a build. When a person wants a new image is a decision about a machine, not a fact about which files a pull request touched. Dispatch says that and a filter cannot.

The image should record which weave it was built from. `packer/weave.pkr.hcl` already tags `WeaveRevision` from a `weave_revision` variable, and `mise/tasks/packer/build` already stamps the binary `v0.0.0-ami.$REVISION`. Add a `weave_version` variable tagged as `WeaveVersion`, defaulting to the same stamp and set to the tag when building from one.

What that tag does not mean has to be said next to it, because it invites the wrong conclusion. The binary in the image is replaced on the next deploy, and is replaced within minutes of the instance booting in the normal case. So `WeaveVersion` says which weave was baked in at image build time. It does not say what the instance is running, it is not a channel, and an image built from `v0.3.0` is a perfectly good host for `v0.4.0`. It is there so an id can be traced to a commit, which is the same job `WeaveRevision` does and the reason that tag exists.

### The build guard, which is the part that surprises people

PR 628 made the panel refuse an engine running other code. `sameBuild` in `internal/cli/serverbuild.go` compares the commit each binary was stamped with, falling back to the version string only when neither side carries a commit. `requireEngineBuild` runs as the remote panel opens, and on a mismatch it draws a screen naming both builds instead of the panel.

The consequence is that a channel is not a property of a binary. It is a property of the panel-and-engine pair, and the pair has to agree on a commit.

On one machine this is invisible and always will be. The panel and the engine are the same process started from the same file, so they cannot disagree, and a user on the stable channel simply curls the `v0.3.0` binary and is done.

With a remote engine it is the whole story. A user on the stable channel has to install the panel binary from the same release the engine is running. Both are built from the tag, `go_build_binary` stamps `stampedCommit` from `build_commit()`, and CI at a tag has a clean tree, so both carry the tag's commit and `sameBuild` passes. The versions agree too, once `remote:deploy` reads `WEAVE_VERSION`. The rule for that user is one sentence: your `weave` and your engine's `weave` come from the same release, and you upgrade them together.

Which is why the engine following releases is better for that user than the engine following main. Today the engine moves on every merge, so a remote panel is correct only while the user's laptop is at main's exact tip, which is to say almost never, and the fix is to keep chasing. After this the engine sits at a published version until somebody publishes another, so the pair stays in agreement for days at a time and upgrading is a thing you do rather than a thing you keep up with.

Harry's own laptop is the case worth naming explicitly, because it gets slightly worse before it gets better. His panel is a `mise run go:build` from a checkout, so it reports `devel` and carries whatever commit that checkout is on. His local panel is untouched by any of this: same binary, both halves. His remote panel already breaks whenever his checkout is not exactly main's tip, and after this it breaks whenever his checkout is not exactly the last release, which will be most of the time because he works on main. The escape hatch is the one that already exists and does not change: `mise run remote:deploy` from his checkout puts his own commit on the box and the pair agrees again. That is what that task is for, and this RFC deliberately leaves it alone.

### What stays dormant until the repository is public

The repository is private. Three things here are affected and none of them block the rest.

Release assets on a private repository need an authenticated request, so `curl -fsSL -o ~/.local/bin/weave https://.../releases/download/v0.3.0/weave-darwin-arm64` answers 404 directly, with no redirect to follow, rather than a binary. The single-curl install in RFC 0018 is already dormant for exactly this reason, and the stable channel is dormant in exactly the same way and to exactly the same degree. Nothing is made worse. `docs/install.md` should describe the stable URL beside the `tip` one and carry the same caveat both already deserve.

Rulesets answer 403. Asking for them on this repository returns `Upgrade to GitHub Pro or make this repository public to enable this feature.` So "main is always releasable" cannot be enforced by a required-status-check ruleset naming `ci.yml`'s `required` gate. It is a convention until the repository is public, held up by the fact that merges run the full suite and by `release:cut` refusing to tag a commit whose CI is not green. That check in the task is the one piece of enforcement available today, which is a reason to have it rather than to leave it to the workflow.

Everything else works while private. Tags, releases and `--generate-notes` are ordinary repository operations, and `main-release.yml` already creates and edits a release on this private repository on every merge. The engine deploy and the image build run under a workflow's own credentials and never touch the download path. So the channel exists, produces real releases with real changelogs, and deploys the engine from them, from the day this lands. What waits for public is only the last step of somebody else installing it, which was already waiting.

## Alternatives

A `stable` branch that main is merged into, with the release cut from its tip. This is the shape most projects end up with and it is the one to reach for the moment a fix has to ship without everything else on main. It costs a merge policy, a backport story, and a second thing that can be behind. Weave has one maintainer and no users, so it would be paying for a problem it does not have. A tag on main is the same guarantee with none of that, and moving to a branch later is not blocked by anything here.

Keep deploying the engine on every merge and gate it on something finer, such as a label or a path filter. This keeps the engine current, which is a real benefit and is why it was written that way. It does not fix the actual complaint: the deploy is a service restart on a shared machine, and no filter makes a merge into a decision to restart it. The label version is a decision to ship wearing a merge's clothes, which is worse than a tag because it is invisible in the history.

GoReleaser for the tag channel, which RFC 0006 proposed and RFC 0009 assumed, both of them treating it as the thing that would produce tagged archives, checksums, a changelog and the Homebrew cask together. It earns its keep on exactly those four. Of those, this channel wants the changelog, which `gh release create --generate-notes` provides, and the checksums, which `mise/tasks/go/release` already writes. `go:release` works, is tested by every merge through `tip`, and produces exactly the raw binaries RFC 0018 argued for. Adding a config file whose output would have to be made to match is work for no gain. When the cask lands, GoReleaser lands with it and gets a whole RFC.

Trigger the engine deploy from `release: published` in a workflow of its own. Cleaner to read, and it costs a personal access token, for the reason given above.

Cut the release from a `workflow_dispatch` that takes a version and creates the tag in CI, rather than from a local task. It removes the "push the tag from your laptop" step, and it runs into the same token wall: a tag created by CI under `GITHUB_TOKEN` starts nothing, so the release would have to be one monolithic job or be given a PAT. Tagging locally also keeps the check that the tag names a commit the person is actually looking at.

## Risks and open questions

Nobody is forced onto the stable channel, and `tip` is the only URL `docs/install.md` has ever printed. Until that document names both and says which one a person should want, the stable channel exists and has no users, which is a documentation job rather than a design one but is the most likely way this quietly fails.

`--generate-notes` between the first tag and nothing has no previous release to diff against, so `v0.1.0`'s changelog will be either every commit in the repository or empty, depending on how GitHub resolves the missing base. Worth finding out on the first real tag rather than guessing. `gh release create` carries `--notes-start-tag`, so the first release can be pointed at a commit or written by hand if what it generates is unreadable.

The engine now sits at a release while main moves ahead of it, so a fix merged to main is not on the engine until somebody cuts a release. That is the intended trade and it is also the thing that will be annoying in practice, since the person who wants the fix is usually the person who wrote it. `mise run remote:deploy` is the answer and it is one command, but it is a command somebody has to remember, and the failure mode is quietly running an engine nobody can name.

Nothing here prunes releases. `tip` is constant storage because it replaces its own assets; tagged releases accumulate five assets each, about 150 MB per release. That is fine for years and is worth knowing about rather than discovering.

Whether a released binary should notice that a newer release exists is out of scope and is the obvious next thing. RFC 0018 sketches a cached daily check against the releases API and notes it is the only mechanism `tip` could have. A tagged channel is where that check actually means something, because there is a version to compare and a changelog to point at, and it needs the repository to be public to be worth writing.
