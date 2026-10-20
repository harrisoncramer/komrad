ALTER TABLE review_rounds RENAME TO review_snapshots;
ALTER TABLE review_snapshots RENAME COLUMN snapshot_sha TO sha;
ALTER TABLE reviews RENAME COLUMN round_id TO snapshot_id;
ALTER TABLE review_threads RENAME COLUMN round_id TO snapshot_id;
ALTER TABLE review_messages RENAME COLUMN round_id TO snapshot_id;
ALTER TABLE agents RENAME COLUMN moved_since_round TO moved_since_snapshot;
ALTER TABLE tasks RENAME COLUMN moved_since_round TO moved_since_snapshot;

ALTER INDEX review_rounds_task_ordinal RENAME TO review_snapshots_task_ordinal;
ALTER INDEX reviews_round_id RENAME TO reviews_snapshot_id;

ALTER TABLE review_snapshots RENAME CONSTRAINT review_rounds_pkey TO review_snapshots_pkey;
ALTER TABLE review_snapshots RENAME CONSTRAINT review_rounds_engine_id_id_key TO review_snapshots_engine_id_id_key;
ALTER TABLE review_snapshots RENAME CONSTRAINT review_rounds_engine_id_fkey TO review_snapshots_engine_id_fkey;
ALTER TABLE review_snapshots RENAME CONSTRAINT review_rounds_task_id_fkey TO review_snapshots_task_id_fkey;
ALTER TABLE review_snapshots RENAME CONSTRAINT review_rounds_id_not_null TO review_snapshots_id_not_null;
ALTER TABLE review_snapshots RENAME CONSTRAINT review_rounds_task_id_not_null TO review_snapshots_task_id_not_null;
ALTER TABLE review_snapshots RENAME CONSTRAINT review_rounds_ordinal_not_null TO review_snapshots_ordinal_not_null;
ALTER TABLE review_snapshots RENAME CONSTRAINT review_rounds_base_sha_not_null TO review_snapshots_base_sha_not_null;
ALTER TABLE review_snapshots RENAME CONSTRAINT review_rounds_snapshot_sha_not_null TO review_snapshots_sha_not_null;
ALTER TABLE review_snapshots RENAME CONSTRAINT review_rounds_created_at_not_null TO review_snapshots_created_at_not_null;
ALTER TABLE review_snapshots RENAME CONSTRAINT review_rounds_engine_id_not_null TO review_snapshots_engine_id_not_null;
ALTER TABLE reviews RENAME CONSTRAINT reviews_round_id_fkey TO reviews_snapshot_id_fkey;
ALTER TABLE reviews RENAME CONSTRAINT reviews_round_id_not_null TO reviews_snapshot_id_not_null;
ALTER TABLE review_threads RENAME CONSTRAINT review_threads_round_id_fkey TO review_threads_snapshot_id_fkey;
ALTER TABLE review_threads RENAME CONSTRAINT review_threads_round_id_not_null TO review_threads_snapshot_id_not_null;
ALTER TABLE review_messages RENAME CONSTRAINT review_messages_round_id_fkey TO review_messages_snapshot_id_fkey;
ALTER TABLE agents RENAME CONSTRAINT agents_moved_since_round_not_null TO agents_moved_since_snapshot_not_null;

-- Set comment to column: "moved_since_snapshot" on table: "agents"
COMMENT ON COLUMN "agents"."moved_since_snapshot" IS 'The commit of the agent''s newest review snapshot when its worktree no longer hashes to it, empty while it still does or when there is no snapshot. The cwd syncer rewrites it every pass.';
-- Set comment to column: "snapshot_id" on table: "review_messages"
COMMENT ON COLUMN "review_messages"."snapshot_id" IS 'The snapshot an agent''s reply said answers the thread, taken of its worktree as it stood when it replied. Set only on a reply that resolves the thread; null on everything else.';
-- Set comment to table: "review_snapshots"
COMMENT ON TABLE "review_snapshots" IS 'One frozen copy of what an agent has written. A review is always given against a snapshot rather than against the working files.';
-- Set comment to column: "ordinal" on table: "review_snapshots"
COMMENT ON COLUMN "review_snapshots"."ordinal" IS 'Which snapshot this is for its task, counting from one. The panel names the snapshot by this number.';
-- Set comment to column: "base_sha" on table: "review_snapshots"
COMMENT ON COLUMN "review_snapshots"."base_sha" IS 'The commit the agent''s branch started from. It is resolved once, when the snapshot is taken, so later movement on the base branch cannot change what the snapshot says.';
-- Set comment to column: "sha" on table: "review_snapshots"
COMMENT ON COLUMN "review_snapshots"."sha" IS 'A commit covering the agent''s whole checkout, tracked and untracked files alike. A ref under refs/komrad/snapshots keeps it alive so git will not collect it. Writing it leaves the agent''s own branch, index and files alone.';
-- Set comment to column: "landed_at" on table: "review_snapshots"
COMMENT ON COLUMN "review_snapshots"."landed_at" IS 'When the base branch was first seen holding this snapshot whole, or a later snapshot whole, null until then. A landed snapshot''s settled comments are history rather than work still under review.';
-- Set comment to column: "task_id" on table: "review_snapshots"
COMMENT ON COLUMN "review_snapshots"."task_id" IS 'The task whose checkout the snapshot froze.';
-- Set comment to table: "review_threads"
COMMENT ON TABLE "review_threads" IS 'One conversation pinned to a file and a line range in the snapshot its review was given against.';
-- Set comment to column: "quoted_lines" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."quoted_lines" IS 'The diff lines the thread was written against. They still say what the thread was about after the agent has rewritten that code. A later snapshot is searched for these lines to decide whether the thread has gone outdated.';
-- Set comment to column: "snapshot_id" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."snapshot_id" IS 'The snapshot this thread''s line numbers were measured against. It is not read through the review, because a review stays open across several snapshots and would name the wrong one.';
-- Set comment to column: "base_sha" on table: "review_threads"
COMMENT ON COLUMN "review_threads"."base_sha" IS 'The commit the old side of this thread''s line numbers was read against. For a thread written on the whole change that is the snapshot''s own base. For a thread written while the diff was narrowed to one snapshot it is the earlier snapshot''s commit.';
-- Set comment to table: "reviews"
COMMENT ON TABLE "reviews" IS 'One reviewer''s batch of comments on one snapshot. Nobody else sees it while it is being written, and it is released all at once.';
-- Set comment to column: "task_id" on table: "reviews"
COMMENT ON COLUMN "reviews"."task_id" IS 'The task being reviewed. The snapshot already says this, and it is repeated here so that one open review per reviewer can be a plain unique index with no join.';
-- Set comment to column: "moved_since_snapshot" on table: "tasks"
COMMENT ON COLUMN "tasks"."moved_since_snapshot" IS 'The commit of the newest review snapshot of the task''s agents when its checkout no longer hashes to it, null or empty while it still does or when there is no snapshot.';
-- Set comment to column: "fork_point" on table: "tasks"
COMMENT ON COLUMN "tasks"."fork_point" IS 'The fork point, the commit of the parent''s branch this task''s own commits sit on, which snapshots and line counts measure from; null while the task has no parent.';
