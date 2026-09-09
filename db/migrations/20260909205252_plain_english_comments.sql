-- Set comment to table: "agent_metadata"
COMMENT ON TABLE "agent_metadata" IS 'Facts an agent recorded about its own work, such as the pull request it opened.';
-- Set comment to table: "agent_runs"
COMMENT ON TABLE "agent_runs" IS 'One run of a script weave fired for an agent. The output stays on disk and this row says where to find it.';
-- Set comment to table: "agent_transcript_chunks"
COMMENT ON TABLE "agent_transcript_chunks" IS 'One turn of an agent''s conversation, indexed so it can be searched by word.';
-- Set comment to table: "agent_transcripts"
COMMENT ON TABLE "agent_transcripts" IS 'One agent''s whole conversation as plain text, copied from the log its CLI writes on disk. Every agent has exactly one row.';
-- Set comment to table: "agents"
COMMENT ON TABLE "agents" IS 'One background agent, holding everything needed to start it again.';
-- Set comment to column: "state" on table: "agents"
COMMENT ON COLUMN "agents"."state" IS 'What the agent is doing. Weave captures the agent''s terminal every couple of seconds and writes here what it read there. Uncertain means the terminal was captured but could not be understood. The setup and failure values never come from reading a terminal, so something else sets those.';
-- Set comment to column: "reported_reason" on table: "agents"
COMMENT ON COLUMN "agents"."reported_reason" IS 'What the agent''s own CLI last said it was doing. It only decorates the state read off the terminal and never sets that state. It is cleared whenever the state changes and when a turn ends, so an empty value means there is nothing to say.';
-- Set comment to column: "role" on table: "agents"
COMMENT ON COLUMN "agents"."role" IS 'What the agent was launched to do. It decides which review briefing the agent is given. A reviewer reads the work of the agent it is a child of and never writes any code of its own.';
-- Set comment to column: "kind" on table: "mail"
COMMENT ON COLUMN "mail"."kind" IS 'Who wrote the mail and why. Plain is mail an agent sent. The other values are notices weave raised itself, which the panel draws differently. Weave also counts unread notices by this value. One unread notice of a kind is enough however much is waiting behind it.';
-- Set comment to column: "about_agent_id" on table: "mail"
COMMENT ON COLUMN "mail"."about_agent_id" IS 'The other agent a weave notice is about, null on ordinary mail. Counting unread notices by this column gives the recipient one notice per subject rather than one per kind, so an agent that hits the exchange cap with a second partner is told about that partner too. There is no foreign key here on purpose. The notice has to keep naming the agent whose exchange was cut off after that agent is gone.';
-- Set comment to index: "mail_from_agent_id" on table: "mail"
COMMENT ON INDEX "mail_from_agent_id" IS 'Counts the mail two agents have exchanged. A send is refused once that count reaches the cap.';
-- Set comment to index: "mail_pending" on table: "mail"
COMMENT ON INDEX "mail_pending" IS 'Reads the mail an agent has not claimed yet. A send is refused when that is already full, and a further review notice joins the unread one found here.';
-- Set comment to table: "marks"
COMMENT ON TABLE "marks" IS 'A letter a person types to jump to one row. Each letter belongs to a single agent or a single workflow, so it always names one place.';
-- Set comment to table: "review_messages"
COMMENT ON TABLE "review_messages" IS 'One message in a review thread, written either by the reviewer or by the agent answering them.';
-- Set comment to column: "author_kind" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."author_kind" IS 'Whether a person or an agent wrote the message.';
-- Set comment to column: "author_agent_id" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."author_agent_id" IS 'The agent that wrote this message, null when a person wrote it or when that agent has since been deleted.';
-- Set comment to column: "review_id" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."review_id" IS 'The review this message was released with, null for an answer from the agent. An answer belongs to no batch and reaches the reviewer as soon as it is written.';
-- Set comment to column: "suggestion" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."suggestion" IS 'Replacement lines the reviewer is proposing for the range the thread is pinned to, empty on an ordinary comment. The agent applies them as written rather than treating them as a description of what to do.';
-- Set comment to table: "review_rounds"
COMMENT ON TABLE "review_rounds" IS 'One frozen snapshot of what an agent has written. A review is always given against a round rather than against the working files.';
-- Set comment to column: "ordinal" on table: "review_rounds"
COMMENT ON COLUMN "review_rounds"."ordinal" IS 'Which round this is for its agent, counting from one. The panel names the round by this number.';
-- Set comment to column: "base_sha" on table: "review_rounds"
COMMENT ON COLUMN "review_rounds"."base_sha" IS 'The commit the agent''s branch started from. It is resolved once, when the round is taken, so later movement on the base branch cannot change what the round says.';
-- Set comment to column: "snapshot_sha" on table: "review_rounds"
COMMENT ON COLUMN "review_rounds"."snapshot_sha" IS 'A commit covering the agent''s whole checkout, tracked and untracked files alike. A ref under refs/weave/rounds keeps it alive so git will not collect it. Writing it leaves the agent''s own branch, index and files alone.';
-- Set comment to table: "review_threads"
COMMENT ON TABLE "review_threads" IS 'One conversation pinned to a file and a line range in the round its review was given against.';
-- Set comment to column: "agent_id" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."agent_id" IS 'The agent being reviewed. The review already says this and neither value ever changes. It is repeated here because almost every read of threads is by agent.';
-- Set comment to column: "file_path" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."file_path" IS 'The file the thread is pinned to, named the way the diff names it. That is a path relative to the repository root, and where the change left the file if it moved it.';
-- Set comment to column: "old_start" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."old_start" IS 'First line of the range on the old side of the diff. Zero means the thread is pinned to lines that exist only on the new side.';
-- Set comment to column: "old_end" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."old_end" IS 'Last line of the range on the old side. It equals old_start for a thread on a single line.';
-- Set comment to column: "new_start" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."new_start" IS 'First line of the range on the new side of the diff. Zero means the thread is pinned to lines the change deleted.';
-- Set comment to column: "new_end" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."new_end" IS 'Last line of the range on the new side. It equals new_start for a thread on a single line.';
-- Set comment to column: "quoted_lines" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."quoted_lines" IS 'The diff lines the thread was written against. They still say what the thread was about after the agent has rewritten that code. A later round is searched for these lines to decide whether the thread has gone outdated.';
-- Set comment to column: "human_read_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."human_read_at" IS 'When the person reviewing last read this thread. It is copied from last_agent_activity_at rather than taken from the clock, so a reply written while they were reading is not marked read without having been shown.';
-- Set comment to column: "last_agent_activity_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."last_agent_activity_at" IS 'When the agent last wrote or rewrote a message here, null until it has written one. It is a column rather than something read off the messages. An agent that rewrites its own reply is activity the person reviewing has not seen, and comparing against the message''s created_at would miss that.';
-- Set comment to column: "resolved_at" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."resolved_at" IS 'When the person reviewing settled this thread, null while it is still owed an answer. Settling is theirs to do even on a thread an agent opened.';
-- Set comment to column: "round_id" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."round_id" IS 'The round this thread''s line numbers were measured against. It is not read through the review, because a review stays open across several rounds and would name the wrong one.';
-- Set comment to column: "harvested" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."harvested" IS 'Whether weave wrote this thread itself, from an edit made in the round''s checkout, rather than a person or an agent typing it. Such a thread is weave''s to rewrite or delete on a later harvest.';
-- Set comment to column: "base_sha" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."base_sha" IS 'The commit the old side of this thread''s line numbers was read against. For a thread written on the whole change that is the round''s own base. For a thread written while the diff was narrowed to one round it is the earlier round''s snapshot.';
-- Set comment to table: "review_viewed_files"
COMMENT ON TABLE "review_viewed_files" IS 'One file of one round that the person reviewing has read. Rows are per round, so an agent that rewrites the file makes a new round and the file reads as unread again.';
-- Set comment to column: "file_path" on table: "review_viewed_files"
COMMENT ON COLUMN "review_viewed_files"."file_path" IS 'The file that was read, named the way the diff names it. That is a path relative to the repository root, and where the change left the file if it moved it.';
-- Set comment to table: "reviews"
COMMENT ON TABLE "reviews" IS 'One reviewer''s batch of comments on one round. Nobody else sees it while it is being written, and it is released all at once.';
-- Set comment to column: "agent_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."agent_id" IS 'The agent being reviewed. The round already says this, and it is repeated here so that one open review per reviewer can be a plain unique index with no join.';
-- Set comment to column: "author_kind" on table: "reviews"
COMMENT ON COLUMN "reviews"."author_kind" IS 'Whether a person or an agent is giving the review.';
-- Set comment to column: "author_agent_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."author_agent_id" IS 'The agent giving the review, null when a person is giving it. Do not read null as meaning a person, because author_kind is what says that. Deleting the reviewing agent clears this column rather than deleting the review. Reviewer agents are deleted as a matter of course, and the agent they reviewed is still working from what they said.';
-- Set comment to column: "verdict" on table: "reviews"
COMMENT ON COLUMN "reviews"."verdict" IS 'What the reviewer concluded, null while the review is still open. It is how the agent learns whether the work is done, and only an approval lets it open a pull request.';
-- Set comment to column: "released_at" on table: "reviews"
COMMENT ON COLUMN "reviews"."released_at" IS 'When the review was released, null while it is still being written. None of it reaches the agent until this is set.';
-- Set comment to column: "path" on table: "reviews"
COMMENT ON COLUMN "reviews"."path" IS 'The absolute path of the file the released review was written to, empty while the review is still open. The file lives in weave''s own state directory rather than in the agent''s checkout.';
-- Set comment to column: "doorbell_mail_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."doorbell_mail_id" IS 'The mail that told the agent a review was waiting. Deleting that mail clears this column rather than deleting the review, so a review whose notice was thrown away reads as undelivered.';
-- Set comment to column: "body" on table: "reviews"
COMMENT ON COLUMN "reviews"."body" IS 'What the reviewer said about the change as a whole rather than about any one line, empty when they said nothing. It goes at the top of the released review file, above the line comments, because it is what the agent should read first.';
-- Set comment to index: "reviews_doorbell_mail_id" on table: "reviews"
COMMENT ON INDEX "reviews_doorbell_mail_id" IS 'Finds the review a piece of mail was the notice for. Deleting that mail walks this index.';
-- Set comment to index: "reviews_open_agent" on table: "reviews"
COMMENT ON INDEX "reviews_open_agent" IS 'Holds each reviewing agent to one unreleased review at a time. Postgres counts two nulls in this column as different values, and the index relies on that. If nulls collided, deleting a second reviewer agent would fail, because the foreign key clears this column and would land on the null the first delete left behind.';
-- Set comment to index: "reviews_open_human" on table: "reviews"
COMMENT ON INDEX "reviews_open_human" IS 'Holds the person to one unreleased review at a time, the way GitHub holds one pending review per reviewer.';
-- Set comment to table: "schedules"
COMMENT ON TABLE "schedules" IS 'A recurring agent launch, holding when to fire and the prompt to launch with.';
-- Set comment to table: "workflow_configs"
COMMENT ON TABLE "workflow_configs" IS 'The harness, working directory and flags a workflow gives each new agent it creates.';
-- Set comment to table: "workflow_hooks"
COMMENT ON TABLE "workflow_hooks" IS 'Scripts a workflow runs at one point in an agent''s life, in position order.';
-- Set comment to table: "workflow_provisioner_configs"
COMMENT ON TABLE "workflow_provisioner_configs" IS 'Whether a workflow gives each of its agents a git worktree of its own, and how that worktree is set up.';
-- Set comment to table: "workflows"
COMMENT ON TABLE "workflows" IS 'A named group of related agents. It is grouping only. Nothing here runs.';
