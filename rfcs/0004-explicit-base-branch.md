# RFC 0004: Every workflow names its base branch

## Problem

A workflow's `worktree.baseBranch` is optional. When it is blank, every place that needs a base works one out for itself, and each does it differently. The one they mostly agree on is the clone's `refs/remotes/origin/HEAD`, which is a symbolic ref `git clone` writes once and nothing else maintains. A clone made by `git init` and `git fetch`, a clone whose remote head was deleted, or a clone whose forge has since renamed its default branch does not have it, or has it wrong.

Last night the engine's clone had no `origin/HEAD`. `plan_rebase` refused every agent in the workflow with `the base branch does not resolve to a remote-tracking ref: resolved to "HEAD"`, and the orchestrator repaired it by hand with `git remote set-head origin -a`. Nothing in the panel said which workflow setting was missing, because none was: the setting was blank, as it is allowed to be.

### Where a base is resolved today

Every path below reads `flow.Worktree.BaseBranch` and passes it, possibly blank, into one of these.

**`resolveBase` in `internal/git/repo.go`** is the one function under all the Go paths:

```go
if base == "" {
    if head, err := Run(ctx, root, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
        return head
    }
    return "HEAD"
}
if _, err := Run(ctx, root, "rev-parse", "--verify", "--quiet", "origin/"+base); err == nil {
    return "origin/" + base
}
return base
```

A blank base with no `origin/HEAD` resolves to the literal `HEAD`, which is the agent's own branch. What each caller then does with that:

- **Rebase.** `FetchBase` in `internal/git/rebase.go` runs `git fetch origin` with no refspec when the base is blank, resolves, and refuses anything not under `origin/`. That refusal is the error above, and `RebaseService.plan` in `internal/checkouts/rebase.go` passes it straight out of `plan_rebase` and `rebase_worktree`.
- **Worktree inspection.** `Inspector.nameBase` in `internal/worktrees/inspect.go` calls `ResolveBase` (`internal/git/standing.go`) without a fetch, which returns `ok == false` for `HEAD`, so `get_worktree` and the worktrees screen show no base, no ahead or behind, and no commits. `baseOf` also answers blank for a directory whose workflow row is gone, which lands on the same fallback.
- **Cwd sync.** `CwdSyncer.listBases` in `internal/checkouts/cwd_sync.go` reads every workflow's base and `ReadStanding` in `internal/git/snapshot.go` resolves it. `isOwnHead` treats `HEAD` as the base, so every agent with a reviewed round reads as moved the moment it edits anything, whether or not the base already holds its work.
- **Review rounds.** `ReviewService.OpenRound` in `internal/reviews/rounds.go` passes the base to `TakeSnapshot`, which runs `merge-base <base> HEAD`. With the base resolved to `HEAD` that is the agent's own tip, so the round's `BaseSHA` is the branch head and the reviewer's diff holds only the uncommitted files. A review given on such a machine reads none of the branch's commits and nobody is told.
- **Provisioning.** `_worktree_setup` in `internal/scripts/builtin` gets `WEAVE_WORKTREE_BASE_BRANCH` from `internal/scriptrun/env.go` and, when it is blank, guesses in four steps: `origin/HEAD` in the clone, then `main` or `master` as a remote or local ref, then `git ls-remote --symref origin HEAD` over the network, then the branch the checkout is on. When it asked origin it writes the answer back with `git remote set-head`, so this script is the only thing in weave that ever creates `origin/HEAD` on a clone that lacked it. Eighty lines of the script and nine of its tests exist to make that guess.
- **Pull request sync.** The query in `db/queries/pullrequest.sql` that attaches pull requests to agents excludes an agent sitting on the base branch itself, with `c.worktree_base_branch = '' OR a.branch <> c.worktree_base_branch`. A blank base disables the exclusion.

The proto comment on `base_branch` in `proto/proto/workflow/v1/workflow.proto` says blank means "the checkout's current head", the panel form's placeholder in `internal/ui/workflowforms.go` says "repo's default branch", and the setup script says something else again. Three readers, three meanings.

### The tests

`TestFetchBaseRepairsAMissingOriginHead` in `internal/git/rebase_test.go` deletes `origin/HEAD`, calls `FetchBase` with a blank base and asserts `"the unqualified fetch put origin/HEAD back"`. That is not something `FetchBase` does. It is something `git fetch` started doing in git 2.48, and this machine runs 2.43, so the test fails here with the same error the orchestrator saw. The repair the test pins is a property of the git on the machine, which is the whole problem in one assertion.

The other four failures in `internal/git` on this machine (`TestARebaseMovesTheTree`, `TestALandedRoundWithNothingOnTopIsLanded`, `TestALandedRoundWithNewWorkOnTopIsLandedAndMoved`, `TestABranchBehindTheBaseTipIsStillClean`) are a different thing: `isCleanOnto` in `snapshot.go` hands `git merge-tree` a tree as one side, which git accepts from 2.45 and 2.43 refuses with `expected commit type`. They are named here so nobody reads them as evidence for this RFC. They are not, and this proposal does not fix them.

## Proposal

A workflow always names its base branch. The name is chosen once, when the workflow is made, and stored. Every base resolution reads the stored name and nothing reads `origin/HEAD`. A base that origin no longer has is reported as that, naming the workflow, rather than being replaced with a guess.

## Details

**Choosing the name.** `create_workflow` and the panel form take a base branch. When it is left blank the server fills it in, once, in this order, and the response and the form say which was used:

1. `git ls-remote --symref origin HEAD` in the workflow's `cwd`, which asks the forge what the default branch is right now.
2. The branch `cwd` is on, when it has no `origin` to ask.
3. `main`, when `cwd` is not a repository or the remote did not answer.

The answer is stored in `worktree_base_branch` and never re-read from git. There is no config key for it: a default in `config.toml` would be one answer for every repository on the machine, and a repository whose default is `master` or `trunk` would be wrong from then on with nothing saying so. `update_workflow` writes every field, so a blank base there is `invalid_argument`.

**Resolving it.** `resolveBase` takes a name and answers `origin/<name>` when the clone has that remote-tracking ref, `refs/heads/<name>` when the clone has no `origin` at all, and otherwise a new `ErrBaseMissing` saying `origin has no branch named <name>; set the workflow's base branch to one it has`. The `HEAD` arm goes. `ResolveBase` returns the error instead of a boolean, and `FetchBase` fetches `origin <name>` by name every time, so a fetch that fails because origin has no such branch is the same error one step earlier.

**Each caller.** The rebase, inspector, cwd sync and review round paths change only in what they do with `ErrBaseMissing`: `plan_rebase` and `rebase_worktree` refuse with it, `get_worktree` and the worktrees screen show it in place of the standing, cwd sync records it as the agent's standing error the way it already records a tree it could not read, and `open_review_round` refuses with it rather than taking a round whose base is the branch's own tip. The `_worktree_setup` script loses the guessing block, `origin_head` and `record_head`: a blank `WEAVE_WORKTREE_BASE_BRANCH` exits 1 saying the workflow names no base, and the rest of the script, which fetches a named base and stages it under `refs/weave/base/<agent>` for single-branch clones, stays as it is. The pull request query drops its blank arm.

**`FetchBase`'s repair.** Not needed anywhere. Nothing reads `origin/HEAD` after this change, so whether a fetch writes it back is of no interest, and `git remote set-head` disappears from the tree.

**`weave doctor`.** A new `bases` check on the engine, after `database`: for every live workflow whose `cwd` is a repository with an `origin`, `origin/<base>` is a ref the clone holds. A missing one warns, naming the workflow and the branch, with the advice to fetch or to update the workflow. No network: the check reads the clone the way `ResolveBase` does, so what it reports is what the panel would refuse.

**MCP and the form.** `create_workflow`'s `worktree.baseBranch` description says the branch worktrees are cut from and every agent's work is measured against, filled in from the remote when left blank. The panel form makes the field required on edit and prefills it on create with the resolved name, and the placeholder `repo's default branch` goes with the behaviour it described. The proto comment is corrected to match.

**Tests.** `TestFetchBaseRepairsAMissingOriginHead` is deleted, since the behaviour it pins is gone. `TestFetchBaseRefusesABaseThatResolvesToHead` becomes a test that a base origin does not have is refused with `ErrBaseMissing`. `TestFetchBaseFetchesWhenNoBaseIsNamed` becomes the named-base case it already nearly is. The script tests that cover guessing (`TestBuiltinWorktreeSetupBaseBranch`'s blank case, the `set-head` cases and `TestBuiltinWorktreeSetupRefusesRatherThanGuessingTheBase`) are replaced by one that a blank base is refused. One new test covers `create_workflow` filling the base in from `ls-remote`, against a bare origin the way `newClonePair` builds one.

## Alternatives

**Repair `origin/HEAD` in `ResolveBase` as well as `FetchBase`.** `ResolveBase` is the no-network path, read on every cwd sync pass and every worktrees screen draw, and setting `origin/HEAD` needs `ls-remote`. It would also keep a stale answer: `origin/HEAD` is a cache of what the forge said on the day of the clone, and a renamed default branch leaves it pointing at a branch that is no longer there.

**Keep the fallback and log when it is taken.** The log is `/var/lib/weave/cache/weave.log`, which nobody reads until something is already wrong. The three meanings of blank would still be three, and a review round taken against the branch's own tip would still be a silent empty diff.

**A config key for the default base.** One value for every repository the machine holds. Wrong for any repository that does not use it, and a second place a person has to know to look.

**Ask the remote at each use.** Correct and slow: a network round trip inside cwd sync and the worktrees screen, and a panel that stops working when the forge is unreachable.

## Migration

One migration in `db/migrations`:

```sql
UPDATE workflow_provisioner_configs SET worktree_base_branch = 'main' WHERE worktree_base_branch = '';
ALTER TABLE workflow_provisioner_configs
    ALTER COLUMN worktree_base_branch DROP DEFAULT,
    ADD CONSTRAINT worktree_base_branch_named CHECK (worktree_base_branch <> '');
```

The column is already `NOT NULL`, so the constraint is that it is not blank. A backfill from each workflow's own `origin/HEAD` would be better than `main`, but a migration cannot run git, and a one-off pass at server start is more machinery than the rows deserve: weave has no users, and the `bases` doctor check names any row the backfill got wrong. `mise run gen` regenerates `db/models` and the `pullrequest.sql` query is edited with it.

## Risks and open questions

- **A workflow whose `cwd` has no `origin`.** Step 2 stores the local branch, and `resolveBase` measures against `refs/heads/<name>`. That is what `client.Rebase(ctx, repo, "main")` already does in the tests, and it is the right answer for a repository that has never been pushed.
- **The forge renames its default branch.** The stored name goes stale and the next fetch reports it. That is the intended behaviour: a person updates the workflow, once, and the panel says so until they do. The old script noticed the rename and guessed the new name; this proposal chooses telling over guessing.
- **`ls-remote` at creation needs credentials.** A remote that refuses or hangs falls through to step 3 and the response says `main` was assumed. The form can be corrected on the spot.
- **Workflows whose `cwd` is not a repository** carry a base nobody reads. That is a stored string, and cheaper than a nullable column every reader has to check.
- **Open:** should `update_workflow` changing the base of a workflow with live agents warn that their standings will be re-read against a different branch on the next pass? Probably as a line in the response rather than a refusal.

## What this does not do

It does not give an agent a base of its own, so an adopted agent on a branch cut from somewhere else is still measured against the workflow's base. It does not detect a renamed default branch and follow it. It does not change how the setup script fetches a named base or stages it per agent for single-branch clones. It does not touch the pull request's own `base_branch`, which is what the forge says the pull request targets and is a different thing.
