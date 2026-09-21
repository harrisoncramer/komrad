# RFC 0003: Deliver a review over the API, not a file

Status: proposed

## Problem

A review reaches the agent it is about through a file on disk. Everything else about a review already goes over the API.

The threads live in Postgres and come back over RPC: `list_review_threads`, `add_review_message`, `resolve_review_thread`, `list_reviews`, `list_review_rounds`. The diff is read from git against a round's snapshot, which is a commit in the agent's own repository. But when a reviewer calls `release_review`, the service renders every comment into one markdown file under its state directory and mails the agent the path:

```go
// internal/reviews/rounds.go
path, err := s.writeReview(ctx, *agent, *round, *open, params, threads)
...
doorbell, err := s.mailer.SendNotice(ctx, mail.SendNoticeParams{
    ToAgentID: params.AgentID,
    Body:      reviewNotice(round.Ordinal, params.Verdict, comments, path),
    Kind:      mailstore.KindReviewActivity,
})
```

```go
func reviewNotice(ordinal int32, verdict reviewstore.Verdict, comments int, path string) string {
    return fmt.Sprintf("weave: review of round %d, %s, %d comment(s), read %s",
        ordinal, strings.ReplaceAll(string(verdict), "_", " "), comments, path)
}
```

The file is written to `<stateDir>/reviews/<agentID>/round-<N>-<reviewID>.md` and its path is stamped on the `reviews.path` column, whose comment records that it moved once already, from the worktree into the state directory. `renderReview` in `internal/reviews/reviewfile.go` draws six lines of the snapshot either side of each comment, marks the commented lines with `>>`, prints the thread id, and ends with "Answer by writing in each thread, naming its id." Appendix A has the whole render.

That file is a third channel carrying what the other two already hold, and it costs us in four places.

**It is why the briefing is long.** The whole of "When you are the one being reviewed" in `internal/agents/briefing.md` is about the file:

> You hear about it as ordinary mail naming a file. Read that file: it holds the whole review, so there is nothing to go looking for. Each comment quotes the code around it as the snapshot held it, with the lines it is about marked.
>
> Those line numbers are true of the snapshot rather than of your files now, so once you have edited, search for the quoted text rather than trusting the number. Answer in the threads with add_review_message, naming the thread id, and say what you did.
>
> The verdict says where you stand. [...] Every release is its own mail, so read every file your mail names before deciding where you stand [...]

Three of those sentences exist only to bridge the file back to the threads: read the file, then find the thread id in it, then answer somewhere else. The `list_review_threads` tool description says the same thing from the other side: "The review you were sent is already in the file the mail named, so read that first; this is for looking a thread up again."

**It has one reader, and only that reader can reach it.** `writeReview` calls `os.WriteFile` on the service's disk and the mail hands the agent an absolute path. The reviewed agent can open it because the service spawned it into a pane on the same machine, so today the disk is shared by construction. Nobody else is on that disk. `list_reviews` drops the path "for anyone but the agent that wrote that review", so an agent cannot find another agent's review file, and a person is never told where one is. The panel's review screen never reads it: `internal/ui/reviewrpc.go` calls `ListReviews` and `ListReviewThreads` and draws the diff from git, and #765 had to carry that git over the engine's transport because the panel runs on a laptop the engine's paths mean nothing on. A harness weave did not launch, or a person wanting the page on their own machine, has the same problem and no transport to fix it with. The file's only reader has MCP, and every reader it might one day have does not have the disk.

**It says less than the threads do.** The file is a snapshot of the release. A thread keeps moving after that: the agent answers, the reviewer answers back, a thread is resolved. The file never learns any of it, and it has no way to say a thread is settled, so the briefing has to warn the agent to "read every file your mail names" and then go to the threads anyway.

## Proposal

Drop the file. A release sends mail saying there is a review, what it concluded and how much it holds, and the agent reads the review over the API it already talks to.

The release mail becomes:

```
weave: review of round 2 released: changes requested, 3 comments. Call get_review with review id 01a0… to read it, then answer each thread with add_review_message.
```

`get_review` is new. It hands back one released review whole: the verdict, what the reviewer said about the work as a whole, then each thread with its anchor, the lines it quotes, the snapshot context around them, and the messages that belong to this release. `list_review_threads` stays as it is, for looking one thread up again.

The briefing paragraph is replaced. `ReleaseReviewResponse.path`, `Review.path` and the `reviews.path` column go. `renderReview` survives as the renderer behind an optional `weave review export`, which writes the same markdown wherever a person asks for it and is never the delivery channel.

## Details

### What the mail says

The mail is the only thing weave types at an agent, so it has to carry enough to act on and nothing that needs a filesystem. Today it names round, verdict, count and path. It should name round, verdict, count, the review id and the tool to call:

```
weave: review of round %d released: %s, %d comment(s). Call get_review with review id %s to read it, then answer each thread with add_review_message.
```

An approval with no comments reads the same way, with "0 comments", and the agent calls `get_review` to read the body if there is one. That keeps one shape for every release rather than a second wording for the happy path.

The mail stays `KindReviewActivity`, and the release still points at it as the doorbell that marks the review delivered when the agent claims it. None of `doorbell_mail_id`, `delivered_at` or `open_threads` changes.

### The replacement briefing paragraph

This replaces the three paragraphs of "When you are the one being reviewed" in `internal/agents/briefing.md`:

> You hear about a review as mail naming a review id and a verdict. Call get_review with that id: it hands back the whole review, the reviewer's note on the work as a whole and then each thread, with the lines it is about quoted as the snapshot held them and the code around them. Those lines are true of the snapshot rather than of your files now, so once you have edited, search for the quoted text rather than counting lines. Answer in each thread with add_review_message, naming its id, and say what you did.
>
> The verdict says where you stand. Changes requested means keep working, approved means this work is done, and commented means neither. Every release is its own mail, so read every review your mail names before deciding where you stand: an approval from one reader does not settle another's changes requested.

The `list_review_threads` description drops "The review you were sent is already in the file the mail named, so read that first" and says instead "The review you were sent is read whole with get_review; this is for looking a thread up again, and for answering one." The `release_review` description drops "Weave writes every comment out as one file and mails that agent the path" for "Weave mails that agent to say the review is there, and it reads the review with get_review, so the whole review arrives as one thing rather than a comment at a time."

### Does `list_review_threads` answer enough today?

Nearly, and the gap is the reason for `get_review` rather than a briefing change alone.

What it has: every thread on the agent, each with `round_id`, `round_ordinal`, `base_sha`, `file_path`, the four line numbers, `quoted_lines`, `resolved_at` and its messages oldest first, with `suggestion` on each message. For an agent reading its own work, `releasedMessages` in `internal/reviews/review.go` filters to messages whose review has been released, so nothing in a batch still being written leaks. That covers "file, line range, quoted lines, messages, resolution".

What it lacks:

1. **Which messages belong to this release.** A thread is opened in one review but a reviewer's later reply lands in the same thread under a later review. `ReviewThread.review_id` names the batch the thread was opened in; `ReviewMessage` has no `review_id` at all. The file-writer gets this right by filtering on the store's `message.ReviewID`:

   ```go
   // internal/reviews/rounds.go, listBatchThreads
   if message.ReviewID != nil && *message.ReviewID == reviewID {
       said = append(said, message)
   }
   ```

   It looks as though `ReviewMessage.sent_at` against `Review.released_at` would make the same cut, since the store stamps `SentAt` from the release. It does not: `releasedMessages` in `internal/reviews/review.go` clears `SentAt`, `DeliveredAt` and `Delivery` on every message before an agent sees it. So an agent reading the threads over RPC cannot make that cut, and on round three it cannot tell which comments are new without diffing against what it remembers. `get_review` makes the cut on the server, the way the file-writer already does.

2. **The verdict and the body.** Those are on `Review`, which `list_reviews` returns, not on the threads. Two calls where one would do.

3. **The context around the quoted lines.** `quoted_lines` are the diff lines the reviewer passed. The file adds six lines of the snapshot either side, read with `ReadFileLines(ctx, agent.Cwd, round.SnapshotSHA, path)`. The agent could run `git show <snapshot>:<path>` itself, since the snapshot commit is in its repository. But the service already reads it, always shares a disk with the worktree, and a client that does not (the panel, `weave review export` on a laptop) cannot. So `get_review` carries the context, and the render moves from disk to the wire.

4. **The body first.** `ListReviewThreads` in `db/queries/review.sql` already orders by round, file, line and id, which is reading order, and `get_review` keeps that order. What the file adds is the verdict and the body at the head of it, before the first thread. That is a small thing, but it is the whole point of "one thing to read".

So `get_review(review_id)` returns:

```
Review          the verdict, the body, round ordinal, author, released_at
repeated Thread  in the order list_review_threads gives them, each with:
  the ReviewThread as list_review_threads renders it
  repeated string context   the snapshot lines around the range, with the range marked
  messages narrowed to this review
```

`list_reviews` already tells the agent which reviews exist and which is newest. It stays as it is, minus the path.

### A CLI without MCP, or a person at a terminal

Every RPC with `mcp: true` is also `cli: true`, so `weave rpc get-review --review-id …` exists the day the RPC does, like `weave rpc list-review-threads` today. A harness with no MCP client reads the review that way, and the mail wording is the same, since "call get_review" is the tool name whichever way it is called.

A person at a terminal who wants a page to read rather than JSON gets `weave review export <review-id> [path]`, which calls `get_review` and runs `renderReview` on the answer. It works from the panel's machine because everything it needs is in the response. Today's file cannot be produced from anywhere but the engine.

### The reviewer's flow stays whole

Nothing seen until release is a property of the store, not of the file. `create_review_thread` files each comment under the author's open review; `ListReviewThreads` shows a non-human reader only messages whose `SentAt` is set or which it wrote itself; `release_review` stamps the batch. `get_review` refuses a review with no `released_at` for anyone but its author, the same rule `list_reviews` applies now ("A review nobody has released is left out for the same reason"). The reviewer writes as many threads as it likes, releases once, and the agent sees them all at once when it calls `get_review`. The file was never what made the release atomic.

### The panel's review screen does not change

Confirmed by reading it. `internal/ui/reviewrpc.go` loads a review with `ListReviews` and `ListReviewThreads`; `reviewload.go` draws the diff from git, over the engine's transport after #765. Nothing in `internal/ui/review*.go` reads `Review.path` or opens a file. Dropping the field is a proto change the panel does not notice. The panel may later switch to `get_review` for its history view, but nothing here requires it.

### What is lost

A file a person can open in an editor, with the code around each comment and nothing to install. That is real, and it is why `weave review export` keeps `renderReview` alive. But the file as it exists is a poor version of that artefact: nobody but the reviewing agent is told its path, it is written under a state directory no document names, it goes stale the moment anybody answers a thread, and it cannot be produced for a review on a remote engine from the machine a person is sitting at. An export a person asks for, rendered from the API, is a better artefact than a file nobody asks for, written where nobody looks.

## Alternatives

**Keep the file and also send the RPC.** Two sources of truth for one release, and the briefing has to say which one wins. The file would still be stale after the first answer and still unreadable off the engine. This is the fallback step in the migration, for one release, and not a place to stop.

**Put the whole review in the mail body.** Mail holds 10,000 characters and a review with a dozen threads and six lines of context each runs past that. A truncated review is worse than a pointer to a whole one, and a review id in the mail is never too long.

**Change the briefing only and lean on `list_review_threads`.** The cheapest change and the one that does not work. Without a `review_id` on messages the agent cannot tell round three's comments from round two's, and it has to make two calls to see the verdict beside them. The briefing would grow rather than shrink.

**Have the agent render the file itself from the RPC.** Moves the render into every harness rather than out of the service. The point is that a harness needs one tool call, not a script.

## Migration

Weave has no users, so nothing here keeps compatibility, but agents mid-flight took their briefing at launch and the change lands in the middle of their conversations. The steps are ordered so that a running agent is never sent mail it has no instruction for.

1. **Add the read.** `review_id` on `ReviewMessage`; a `GetReview` RPC with `cli: true, mcp: true, name: "get_review"`, returning the shape above; the snapshot context read moved from `writeReview` into the `get_review` handler; `weave rpc get-review` falls out of the generator. The file is still written and the mail is unchanged. Nothing anybody reads has moved.

2. **Change the mail and the briefing together.** The release mail names the review id and `get_review` and, for this one release, still ends with "or read <path>". `briefing.md` gets the replacement paragraph above; `list_review_threads` and `release_review` get their new descriptions; `docs/agents.md` and the reviewer briefing are read through for any mention of the file. An agent launched before this step has a briefing that says "read that file", and the mail still names one. An agent launched after it has a briefing that says `get_review`, and the mail names that. Both act on what they were told.

3. **Add the export.** `weave review export <review-id> [path]` renders `get_review`'s answer with `renderReview`. `renderReview` loses its dependency on the store types and takes the RPC shapes instead.

4. **Stop writing the file.** Once every agent launched before step 2 is archived, which `list_agents` can confirm, `writeReview` and its `os.WriteFile` go, the "or read <path>" clause goes from the mail, and `ReleaseReviewResponse.path`, `Review.path` and the `reviews.path` column are dropped. `reviewfile.go` becomes the export renderer's file. The tests in `review_test.go` that `os.ReadFile(result.Path)` become tests of `get_review`.

Steps 1 and 2 can land as one pull request if the briefing and the mail move together. Step 4 is the one that has to wait.

## Risks and open questions

**Agents mid-flight.** An agent launched before step 2 has a briefing saying "read that file", and under step 4 it would be mailed a review id and no path. It would go looking for a file, not find one, and most likely fall back to `list_review_threads`, whose description now says to call `get_review`. That is probably fine, but it is a guess about a model's behaviour and the migration is ordered to avoid needing it. The cost is that step 4 waits for a generation of agents to finish, which on this panel is days rather than weeks.

**The mail wording is doing the work the file did.** If an agent reads "call get_review" and calls `list_review_threads` instead, it gets everything except the per-release cut and the context, and its answer still lands in the right threads. The failure is a slower agent, not a lost review.

**Context in the response makes `get_review` heavy.** Six lines either side per thread, per file, is what the file already carries, and a review with fifty threads was already a fifty-thread file. If it becomes a problem, `context_lines` on the request is the knob. The store is not touched by it.

**Who may call `get_review`.** Same rule as `list_reviews`, which is `requireMayRead` in `internal/reviews/rounds.go`: a person, the agent whose work it is, and a child of that agent, which is how the reviewer gets in; a parent may not read its child's reviews, and anyone else is `permission_denied`. A review not yet released is `not_found` for anyone but its author. Worth stating in the proto comment rather than being derived by the reader.

**Does the human reviewer's release change?** No. The panel's `reviewcompose.go` calls the same `ReleaseReview` handler, so a person's release and an agent's release send the same mail and stop writing the same file.

**Is `get_review` a second copy of `list_review_threads`?** It is a projection of the same rows rather than a second store of them. If that still feels like two tools for one thing, the alternative is a `review_id` filter on `list_review_threads` plus `context` on every thread, which is more fields on the common call to spare one uncommon one. I would rather the common call stay small.

## What this does not do

It does not change how a review is written, rounded, released or resolved. It does not change the review screen, the diff, or the doorbell that marks a review delivered. It does not touch `open_review_round`, `create_review_thread`, `add_review_message` or `resolve_review_thread`. It does not decide whether mail should one day be pushed into a running session rather than read at the end of a turn; that is `docs/mail.md`'s note about channels, and the mail here is one line either way. It does not make reviews reachable from a panel that cannot reach the engine at all.

## Appendix A: the file as it is written today

`renderReview` in `internal/reviews/reviewfile.go` produces, for a round with one thread:

```markdown
# Review of round 2

Changes requested. Keep working on this.

On the work of **implementer** (`01a0…`), reviewed by the agent implementer-reviewer.

This review was written against the snapshot `abc1234`, taken from `def5678`. To see exactly what was reviewed, run `git diff def5678… abc1234…`.

## What the reviewer said

The shape is right; two things to fix before it goes.

## internal/reviews/rounds.go:498-502

Thread `01a0…`

    >> 498  	doorbell, err := s.mailer.SendNotice(ctx, mail.SendNoticeParams{
    >> 499  		ToAgentID: params.AgentID,
       500  		Body:      reviewNotice(round.Ordinal, params.Verdict, comments, path),
    ...

The mail is sent before the release is recorded, so a failure here leaves the agent told about a review the panel never drew.

Answer by writing in each thread, naming its id.
```

Every field in that render has a source in `Review`, `ReviewRound` or `ReviewThread` as the proto already defines them, except the six-line context, which `getSnapshotLines` reads from the snapshot with `ReadFileLines`, and the per-release cut of messages, which `listBatchThreads` makes on `message.ReviewID`. Those two are what `get_review` adds to the wire.

## Appendix B: where the file is referred to

- `internal/reviews/rounds.go`: `ReleaseReview`, `reviewNotice`, `writeReview`, `getSnapshotLines`, `reviewDirName = "reviews"`.
- `internal/reviews/reviewfile.go`: `renderReview`, `writeThread`, `writeContext`, `suggestionLead`.
- `internal/reviews/review_test.go`: `os.ReadFile(result.Path)` in the release tests; `reviewfile_test.go` tests the render.
- `proto/proto/agent/v1/review.proto`: `Review.path`, `ReleaseReviewResponse.path`, the `ReleaseReviewRequest` comment ("Weave writes the whole review out as one file beside the agent's work"), the `body` comment ("goes at the head of the file the agent reads").
- `proto/proto/agent/v1/agent.proto`: the `list_review_threads`, `list_reviews` and `release_review` tool descriptions, and their generated copies under `docs/reference/`.
- `db/migrations/20260906123642_review_body.sql`: the `reviews.path` column comment.
- `internal/agents/briefing.md`: "When you are the one being reviewed".
- `internal/mail/deliver.go`: not the file, but the nudge that gets the mail read; it types `weave: %d piece(s) of unread mail, call read_mail` and nothing about reviews, and it does not change.
