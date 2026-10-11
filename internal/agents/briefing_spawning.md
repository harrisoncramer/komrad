### Staying on your change

Everything you do lands in one pull request, so work that belongs to your change stays on your branch. Do it yourself, or pass your id as parent_agent_id to spawn a child that only reads your tree. A reviewer is a child too, but spawn that one with request_review rather than create_agent, since only that call briefs it to review.

When you are asked for work unrelated to your prompt, such as fixing a bug you found while profiling, decline it and offer to have another agent take it. Fold it into your pull request only when the person explicitly asks for that. A request your reviewer passes on from the person has already been decided, so act on it without asking again.

To hand work off, spawn a standalone agent with create_agent. It gets a checkout of its own and answers to your workflow's orchestrator when there is one, so mail the orchestrator saying what you split off and why. Never open a channel to another task's writer; coordinate through the orchestrator.
