table "forge_connections" {
  schema  = schema.public
  comment = "A forge komrad has been connected to, one row per host, holding who the credential was verified as. The credential itself is a file under komrad's state directory rather than a column."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "kind" {
    null    = false
    type    = text
    comment = "Which forge the host runs: github or gitlab."
  }
  column "host" {
    null    = false
    type    = text
    comment = "The host agents' remotes name, such as github.com or ghe.example.com. It is unique because one machine acts as one identity on one forge."
  }
  column "api_url" {
    null    = false
    type    = text
    default = ""
    comment = "Where the forge answers API calls when it is not the public one, such as https://ghe.example.com/api/v3. Empty for github.com and gitlab.com."
  }
  column "method" {
    null    = false
    type    = text
    comment = "How the credential was obtained: token for one a person supplied, github_app for one minted from the engine's App."
  }
  column "login" {
    null    = false
    type    = text
    comment = "Who the forge said the credential belongs to when it was verified, a user login or an App slug."
  }
  column "verified_at" {
    null    = true
    type    = timestamptz
    comment = "When the forge last confirmed the credential. Cleared when the forge refuses it, so the row says the connection needs remaking."
  }
  column "created_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "throttled_until" {
    null    = true
    type    = timestamptz
    comment = "When the forge said this credential's rate limit resets, after a refusal or a response whose remaining budget was zero. The pull request sync makes no request through the connection until it passes."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  foreign_key "forge_connections_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  unique "forge_connections_host_key" {
    columns = [column.engine_id, column.host]
  }
}

table "pull_requests" {
  schema  = schema.public
  comment = "A pull request komrad has read from a forge, one row per forge identity and none owned by an agent. Finished is derived: merged when merged_at is set, closed when closed_at is set and merged_at is not, open otherwise."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "host" {
    null    = false
    type    = text
    comment = "The forge host the repository lives on, such as github.com, taken from the repository URL. Two installations of one forge hand out overlapping identifiers, so the host is part of the identity."
  }
  column "forge" {
    null    = false
    type    = text
    comment = "Which kind of forge the host runs: github or gitlab."
  }
  column "provider_id" {
    null    = false
    type    = text
    comment = "The forge's own stable identifier: GitHub's node_id, GitLab's global id. A rename on the forge changes the URL and not this."
  }
  column "url" {
    null    = false
    type    = text
    comment = "The web address a person opens. Unique, and a locator rather than the identity."
  }
  column "repository_url" {
    null    = false
    type    = text
    comment = "The canonical URL of the repository the pull request targets."
  }
  column "head_repository_url" {
    null    = false
    type    = text
    comment = "The canonical URL of the repository the branch lives in. Empty when the forge no longer has it, which GitHub answers after a fork is deleted."
  }
  column "number" {
    null    = false
    type    = integer
    comment = "The per-repository number a person sees: GitHub's number, GitLab's iid."
  }
  column "title" {
    null = false
    type = text
  }
  column "head_branch" {
    null = false
    type = text
  }
  column "head_sha" {
    null = false
    type = text
  }
  column "base_branch" {
    null = false
    type = text
  }
  column "base_sha" {
    null = false
    type = text
  }
  column "draft" {
    null = false
    type = boolean
  }
  column "state" {
    null    = false
    type    = text
    comment = "Where the forge says it stands, as OPEN, CLOSED or MERGED, a cross-check on the timestamps."
  }
  column "opened_at" {
    null    = false
    type    = timestamptz
    comment = "When the forge says it was opened, which is what orders the pull requests on one branch."
  }
  column "changed_at" {
    null    = false
    type    = timestamptz
    comment = "The forge's own last-change stamp, which is what the sync pages by. It is the forge's clock and not komrad's, so it is not named updated_at and carries no trigger."
  }
  column "merged_at" {
    null = true
    type = timestamptz
  }
  column "closed_at" {
    null = true
    type = timestamptz
  }
  column "first_seen_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
    comment = "When komrad first inserted the row."
  }
  column "review_decision" {
    null    = true
    type    = text
    comment = "Where the forge says the reviews stand, in komrad's own words: pending, approved or changes_requested, null while it has said nothing."
  }
  column "checks" {
    null    = true
    type    = text
    comment = "Where the forge says the checks on the head commit stand, in komrad's own words: pending, passing or failing, null while it reports none."
  }
  column "checks_sha" {
    null    = true
    type    = text
    comment = "The head commit the checks were read for."
  }
  column "mergeability" {
    null    = true
    type    = text
    comment = "Where the forge says the pull request stands against its base and its branch rules, in komrad's own words: clean, unstable, blocked, behind or conflicting, null while the forge has not computed it. Read with the checks."
  }
  column "last_known_checks" {
    null    = true
    type    = text
    comment = "The checks as the forge last reported them when it reported anything, which a blank reading leaves alone. An event compares against this so that a moment without an answer is not taken for a change."
  }
  column "last_known_checks_sha" {
    null    = true
    type    = text
    comment = "The head commit last_known_checks was read for. The same checks on a new head are a fresh verdict, so an event compares this too."
  }
  column "auto_merge" {
    null    = true
    type    = boolean
    comment = "Whether the forge says the pull request will merge on its own once its checks and reviews pass, null until it has been read. Read with the checks."
  }
  column "auto_mergeable" {
    null    = false
    type    = boolean
    default = false
    comment = "Whether the forge would let komrad's credential turn auto-merge on now, which it will not for a draft, a pull request with nothing left to wait for or a repository that does not allow it."
  }
  column "last_known_mergeability" {
    null    = true
    type    = text
    comment = "The mergeability as the forge last reported it when it reported anything, which a blank reading leaves alone."
  }
  column "last_known_mergeability_sha" {
    null    = true
    type    = text
    comment = "The head commit last_known_mergeability was read for. The panel draws the last known mergeability through a blank reading only while this is still the head."
  }
  column "mergeability_blank_since" {
    null    = true
    type    = timestamptz
    comment = "When the forge first answered a blank mergeability for checks_sha, which bounds how long a blank is read again quickly."
  }
  column "last_known_decision" {
    null    = true
    type    = text
    comment = "The review decision as the forge last reported it when it reported anything, which a blank reading leaves alone."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "pull_requests_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  foreign_key "pull_requests_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  unique "pull_requests_host_provider_id_key" {
    columns = [column.engine_id, column.host, column.provider_id]
  }
  unique "pull_requests_url_key" {
    columns = [column.engine_id, column.url]
  }
  index "pull_requests_unfinished_head" {
    columns = [column.repository_url, column.head_branch]
    where   = "merged_at IS NULL AND closed_at IS NULL"
  }
  index "pull_requests_attribution" {
    columns = [column.repository_url, column.head_repository_url, column.head_branch]
  }
}

table "agent_pull_requests" {
  schema  = schema.public
  comment = "Which agents were seen with which pull requests, one row per pair, never re-pointed. The watch carries every pull request with a row here."
  column "agent_id" {
    null = false
    type = uuid
  }
  column "pull_request_id" {
    null = false
    type = uuid
  }
  column "first_seen_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "last_seen_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
    comment = "When the pair was last seen together, moved at most once an hour."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.agent_id, column.pull_request_id]
  }
  foreign_key "agent_pull_requests_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "agent_pull_requests_agent_id_fkey" {
    columns     = [column.engine_id, column.agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "agent_pull_requests_pull_request_id_fkey" {
    columns     = [column.engine_id, column.pull_request_id]
    ref_columns = [table.pull_requests.column.engine_id, table.pull_requests.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "agent_pull_requests_pull_request_id" {
    columns = [column.pull_request_id]
  }
}

table "pull_request_syncs" {
  schema  = schema.public
  comment = "A repository the pull request sync has read at least once, which is what turns its pull request state from unread to none when it has no pull requests."
  column "repository_url" {
    null = false
    type = text
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.engine_id, column.repository_url]
  }
  foreign_key "pull_request_syncs_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
}

table "forge_review_threads" {
  schema  = schema.public
  comment = "A review thread as the forge holds it on a pull request a live agent is linked to, which the comment sync owns and overwrites. Nothing here reaches an agent, and the reader role has no grant on it."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "pull_request_id" {
    null = false
    type = uuid
  }
  column "provider_id" {
    null    = false
    type    = text
    comment = "The forge's own identifier for the thread: a review thread's node id, or the comment's own id for a comment on the change as a whole."
  }
  column "path" {
    null    = false
    type    = text
    default = ""
    comment = "The file the thread was written on, empty for a comment on the change as a whole."
  }
  column "line" {
    null    = true
    type    = integer
    comment = "The line of the file the thread was written against, counted in commit_sha on the side old_side names, null for a comment on the change as a whole or on a file."
  }
  column "old_side" {
    null    = false
    type    = boolean
    default = false
    comment = "Whether the line is counted on the old side of the forge's diff rather than the new one."
  }
  column "commit_sha" {
    null    = false
    type    = text
    default = ""
    comment = "The commit the forge counted the line in, empty when it names none."
  }
  column "quote" {
    null    = false
    type    = text
    default = ""
    comment = "The text of the line the thread was written on as the forge's diff drew it, with its diff marker taken off."
  }
  column "url" {
    null    = false
    type    = text
    default = ""
    comment = "The web address of the thread's first comment on the forge."
  }
  column "resolved" {
    null    = false
    type    = boolean
    default = false
  }
  column "written_at" {
    null    = false
    type    = timestamptz
    comment = "When the thread's first comment was written on the forge."
  }
  column "dismissed_at" {
    null    = true
    type    = timestamptz
    comment = "When the person dismissed the thread in their review, null while they never have. A dismissed thread is still drawn but no longer counted. The sync leaves it alone, and a comment written on the forge after it makes the thread active again."
  }
  column "created_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "updated_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "forge_review_threads_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  unique "forge_review_threads_provider_id_key" {
    columns = [column.engine_id, column.pull_request_id, column.provider_id]
  }
  foreign_key "forge_review_threads_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "forge_review_threads_pull_request_id_fkey" {
    columns     = [column.engine_id, column.pull_request_id]
    ref_columns = [table.pull_requests.column.engine_id, table.pull_requests.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "forge_review_threads_pull_request_id" {
    columns = [column.pull_request_id]
  }
}

table "forge_review_comments" {
  schema  = schema.public
  comment = "One comment in a forge review thread, which the comment sync owns and overwrites. Nothing here reaches an agent, and the reader role has no grant on it."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "thread_id" {
    null = false
    type = uuid
  }
  column "provider_id" {
    null    = false
    type    = text
    comment = "The forge's own identifier for the comment."
  }
  column "author_login" {
    null    = false
    type    = text
    comment = "The forge login of whoever wrote the comment, empty when the forge no longer knows them."
  }
  column "author_is_bot" {
    null    = false
    type    = boolean
    default = false
    comment = "Whether the forge says the author is a bot."
  }
  column "yours" {
    null    = false
    type    = boolean
    default = false
    comment = "Whether the author is the login the forge connection was verified as."
  }
  column "body" {
    null = false
    type = text
  }
  column "written_at" {
    null    = false
    type    = timestamptz
    comment = "When the comment was written on the forge."
  }
  column "edited_at" {
    null    = true
    type    = timestamptz
    comment = "When the comment was last edited on the forge, null while it never has been."
  }
  column "created_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "updated_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "forge_review_comments_provider_id_key" {
    columns = [column.engine_id, column.thread_id, column.provider_id]
  }
  foreign_key "forge_review_comments_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "forge_review_comments_thread_id_fkey" {
    columns     = [column.engine_id, column.thread_id]
    ref_columns = [table.forge_review_threads.column.engine_id, table.forge_review_threads.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "forge_review_comments_thread_id" {
    columns = [column.thread_id]
  }
}
