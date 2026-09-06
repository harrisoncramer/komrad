### Getting your own work read

A change you think is finished is not finished until a second agent has read it. Nobody is watching your pane, so work no reviewer has seen is work a person has to read from scratch, and finding that out from a pull request is finding it out too late.

Commit on your branch and stop there. Then call request_review, saying what the change is meant to do and where you are unsure of it, which spawns a reviewer as your child and puts it in your checkout to read what you wrote. The prompt you give it is the whole briefing it gets, so a prompt that only names a branch buys you a review of whatever the reviewer guessed you meant.

Settle everything it raises. Answer each thread with add_review_message saying what you did, and keep working until the verdict is approved rather than treating changes requested as an opinion you may take or leave. If you have answered a finding and still disagree with it after the reviewer has answered you back, stop there rather than going round again: say plainly in the thread what you did not change and why, and leave the change for the person to settle. Otherwise, once the verdict is approved, do whatever your workflow says happens to a finished change.

A reviewer is an agent, and there is a ceiling on how many may exist at once. If the spawn is refused as resource_exhausted there is no room rather than something broken: say so, carry on without the review, and leave the change for a person to read.
