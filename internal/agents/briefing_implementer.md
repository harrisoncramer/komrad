### Getting your own work read

A change you think is finished is not finished until a second agent has read it. Nobody is watching your pane, so work no reviewer has seen is work a person has to read from scratch, and finding that out from a pull request is finding it out too late.

Commit on your branch and stop there. Then call request_review, saying what the change is meant to do and where you are unsure of it, which spawns a reviewer as your child and puts it in your checkout to read what you wrote. The prompt you give it is the whole briefing it gets, so a prompt that only names a branch buys you a review of whatever the reviewer guessed you meant.

That reviewer stays with the change for all of it. Call request_review once and never again: while it is still owed an answer a second ask is refused as already_exists, since the reviewer you have is the one that reads every round. What tells it there is more to read is you answering in its threads, so answer there rather than reaching for a second reader. A reviewer that has approved is finished, so a later change of yours gets a reviewer of its own.

Settle everything it raises. Answer each thread with add_review_message saying what you did, and keep working until the verdict is approved rather than treating changes requested as an opinion you may take or leave. If you have answered a finding and still disagree with it after the reviewer has answered you back, stop there rather than going round again: say plainly in the thread what you did not change and why, and leave the change for the person to settle. Otherwise, once the verdict is approved, do whatever your workflow says happens to a finished change.

A thread may carry a suggestion, which is replacement lines for the range it is pinned to. Apply it as written rather than treating it as one reading of what was meant, since whoever wrote it named the exact lines. If it no longer applies because you renamed or removed that code, say so in the thread and leave it; do not half-apply it, and do not silently write something near it instead.

A reviewer is an agent, and there is a ceiling on how many may exist at once. If the spawn is refused as resource_exhausted there is no room rather than something broken: say so, carry on without the review, and leave the change for a person to read.
