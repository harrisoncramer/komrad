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
  column "author" {
    null    = false
    type    = text
    default = ""
    comment = "The login of whoever opened it, empty when the forge no longer has them."
  }
  column "body" {
    null    = false
    type    = text
    default = ""
    comment = "The description as the forge has it, in markdown."
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
  column "latest_decision" {
    null    = true
    type    = text
    comment = "Where the latest read said the reviews stand, in komrad's own words: pending, approved, changes_requested or rereview_requested, null when it said nothing."
  }
  column "latest_head_sha" {
    null    = true
    type    = text
    comment = "The head commit the latest standing read was for, null before the first read."
  }
  column "latest_mergeability" {
    null    = true
    type    = text
    comment = "Where the latest read said the pull request stands against its base and its branch rules, in komrad's own words: clean, unstable, blocked, behind or conflicting, null when the forge had not computed it."
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
  column "known_mergeability" {
    null    = true
    type    = text
    comment = "The last mergeability a read reported, which a blank read leaves alone."
  }
  column "known_mergeability_sha" {
    null    = true
    type    = text
    comment = "The head commit known_mergeability was read for."
  }
  column "mergeability_blank_since" {
    null    = true
    type    = timestamptz
    comment = "When the forge first answered a blank mergeability for latest_head_sha, which bounds how long a blank is read again quickly."
  }
  column "known_decision" {
    null    = true
    type    = text
    comment = "The last review decision a read reported, which a blank read leaves alone."
  }
  column "stack_number" {
    null    = true
    type    = integer
    comment = "The number of the GitHub stack the pull request is in, null while it is in none."
  }
  column "stack_position" {
    null    = true
    type    = integer
    comment = "Where the pull request sits in its GitHub stack, counting from one at the bottom, null while it is in none."
  }
  column "stack_size" {
    null    = true
    type    = integer
    comment = "How many pull requests the GitHub stack holds, null while the pull request is in none."
  }
  column "stacking_refused_at" {
    null    = true
    type    = timestamptz
    comment = "The changed_at the pull request had when GitHub last refused to put it in a GitHub stack, which komrad asks again only once changed_at moves past; null while nothing was refused."
  }
  column "rebase_awaited_since" {
    null    = true
    type    = timestamptz
    comment = "When komrad began waiting on GitHub to rebase the pull request's branch after the pull request under it in its GitHub stack merged, null while komrad waits on nothing."
  }
  column "rebase_awaited_from" {
    null    = true
    type    = text
    comment = "The head commit the pull request had when komrad began waiting on GitHub to rebase it, which GitHub's push moves past; null while komrad waits on nothing."
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

table "pull_request_check_readings" {
  schema  = schema.public
  comment = "What the forge last said about the checks on one head commit of a pull request, kept for every head it was read on until the pull request goes."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "pull_request_id" {
    null = false
    type = uuid
  }
  column "head_sha" {
    null    = false
    type    = text
    comment = "The head commit the checks were read on."
  }
  column "blank" {
    null    = false
    type    = boolean
    comment = "Whether the latest read for this head came back without a check list, which leaves the head's last listed checks in place."
  }
  column "first_read_at" {
    null    = false
    type    = timestamptz
    comment = "When the checks on this head were first read."
  }
  column "read_at" {
    null    = false
    type    = timestamptz
    comment = "When the checks on this head were last read, stamped by every read whether or not anything changed."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "pull_request_check_readings_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  unique "pull_request_check_readings_head_key" {
    columns = [column.engine_id, column.pull_request_id, column.head_sha]
  }
  foreign_key "pull_request_check_readings_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "pull_request_check_readings_pull_request_id_fkey" {
    columns     = [column.engine_id, column.pull_request_id]
    ref_columns = [table.pull_requests.column.engine_id, table.pull_requests.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "pull_request_check_readings_pull_request_id" {
    columns = [column.pull_request_id]
  }
}

table "pull_request_checks" {
  schema  = schema.public
  comment = "One check the forge listed on a head commit of a pull request, as the latest listing for that head said it stands."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "reading_id" {
    null    = false
    type    = uuid
    comment = "The reading of the head commit the check was listed on."
  }
  column "provider_id" {
    null    = false
    type    = text
    comment = "The forge's own identifier for the check run, commit status or pipeline, which a rerun replaces."
  }
  column "kind" {
    null    = false
    type    = text
    comment = "What the forge reported the check as: check_run for a check run, status for an old-style commit status, pipeline for a GitLab pipeline."
  }
  column "source" {
    null    = false
    type    = text
    comment = "The workflow that ran the check, or the app when no workflow did, empty for a commit status."
  }
  column "name" {
    null    = false
    type    = text
    comment = "The check's name, or a commit status's context."
  }
  column "state" {
    null    = false
    type    = text
    comment = "Where the check stands, in komrad's own words: queued or running while in flight, waiting when it cannot start on its own, completed, or unknown when its commit is no longer the head and komrad stopped reading it while in flight."
  }
  column "conclusion" {
    null    = false
    type    = text
    default = ""
    comment = "How a completed check ended, in the forge's own word lowercased, such as success, failure, error, neutral, cancelled, skipped or timed_out, empty until it completes."
  }
  column "started_at" {
    null    = true
    type    = timestamptz
    comment = "When the check started, null before it has."
  }
  column "finished_at" {
    null    = true
    type    = timestamptz
    comment = "When the check completed, null before it has."
  }
  column "required" {
    null    = false
    type    = boolean
    comment = "Whether the pull request's base branch requires the check to pass before merging."
  }
  column "url" {
    null    = false
    type    = text
    default = ""
    comment = "The web address of the check's details or logs, empty when the forge gave none."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  column "run_id" {
    null    = false
    type    = text
    default = ""
    comment = "The forge's own identifier for the run the check ran in, a check suite on GitHub, empty for a commit status."
  }
  column "run_event" {
    null    = false
    type    = text
    default = ""
    comment = "What started the run, such as pull_request or push, empty when no workflow ran the check."
  }
  column "run_started_at" {
    null    = true
    type    = timestamptz
    comment = "When the run the check ran in was created, null for a commit status."
  }
  column "job_id" {
    null    = true
    type    = bigint
    comment = "The forge's own number for the Actions job that ran the check, which its log is fetched by, null for a commit status or a check run no workflow ran."
  }
  primary_key {
    columns = [column.id]
  }
  unique "pull_request_checks_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  unique "pull_request_checks_provider_id_key" {
    columns = [column.engine_id, column.reading_id, column.provider_id]
  }
  foreign_key "pull_request_checks_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "pull_request_checks_reading_id_fkey" {
    columns     = [column.engine_id, column.reading_id]
    ref_columns = [table.pull_request_check_readings.column.engine_id, table.pull_request_check_readings.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "pull_request_checks_reading_id" {
    columns = [column.reading_id]
  }
}

table "pull_request_check_steps" {
  schema  = schema.public
  comment = "One step of a check run, as the latest listing of the check's head commit said it stands."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "check_id" {
    null    = false
    type    = uuid
    comment = "The check run the step belongs to."
  }
  column "reading_id" {
    null    = false
    type    = uuid
    comment = "The reading of the head commit the step's check was listed on."
  }
  column "number" {
    null    = false
    type    = integer
    comment = "Where the step falls in its check run, counting from one."
  }
  column "name" {
    null    = false
    type    = text
    comment = "The step's name."
  }
  column "state" {
    null    = false
    type    = text
    comment = "Where the step stands, in komrad's own words: queued or running while in flight, waiting when it cannot start on its own, completed, or unknown when its commit is no longer the head and komrad stopped reading it while in flight."
  }
  column "conclusion" {
    null    = false
    type    = text
    default = ""
    comment = "How a completed step ended, in the forge's own word lowercased, such as success, failure, cancelled or skipped, empty until it completes."
  }
  column "started_at" {
    null    = true
    type    = timestamptz
    comment = "When the step started, null before it has."
  }
  column "finished_at" {
    null    = true
    type    = timestamptz
    comment = "When the step completed, null before it has."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "pull_request_check_steps_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  unique "pull_request_check_steps_number_key" {
    columns = [column.engine_id, column.check_id, column.number]
  }
  foreign_key "pull_request_check_steps_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "pull_request_check_steps_check_id_fkey" {
    columns     = [column.engine_id, column.check_id]
    ref_columns = [table.pull_request_checks.column.engine_id, table.pull_request_checks.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "pull_request_check_steps_reading_id_fkey" {
    columns     = [column.engine_id, column.reading_id]
    ref_columns = [table.pull_request_check_readings.column.engine_id, table.pull_request_check_readings.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "pull_request_check_steps_reading_id" {
    columns = [column.reading_id]
  }
}

table "pull_request_check_failure_notes" {
  schema  = schema.public
  comment = "A failed check komrad told an agent about, one row per agent, check and finish time, so a failure is mailed once."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "agent_id" {
    null    = false
    type    = uuid
    comment = "The agent that was told."
  }
  column "check_id" {
    null    = false
    type    = uuid
    comment = "The check that failed."
  }
  column "finished_at" {
    null    = true
    type    = timestamptz
    comment = "When the check finished in the failure the agent was told about, so a second failure of the same check is told again. Null when the forge gave no finish time."
  }
  column "noted_at" {
    null    = false
    type    = timestamptz
    default = sql("now()")
    comment = "When the agent was told, on the same clock that stamps the mail it was told in."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  index "pull_request_check_failure_notes_failure_key" {
    unique         = true
    columns        = [column.engine_id, column.agent_id, column.check_id, column.finished_at]
    nulls_distinct = false
  }
  foreign_key "pull_request_check_failure_notes_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "pull_request_check_failure_notes_agent_id_fkey" {
    columns     = [column.engine_id, column.agent_id]
    ref_columns = [table.agents.column.engine_id, table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "pull_request_check_failure_notes_check_id_fkey" {
    columns     = [column.engine_id, column.check_id]
    ref_columns = [table.pull_request_checks.column.engine_id, table.pull_request_checks.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "pull_request_check_failure_notes_check_id" {
    columns = [column.check_id]
  }
}

table "pull_request_check_logs" {
  schema  = schema.public
  comment = "The log of a check's Actions job as komrad last fetched it, one row per check, kept whole and cleaned."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "check_id" {
    null    = false
    type    = uuid
    comment = "The check whose job the log is of."
  }
  column "fetched_at" {
    null    = false
    type    = timestamptz
    comment = "When komrad last asked the forge for the log."
  }
  column "outcome" {
    null    = false
    type    = text
    comment = "What the forge answered: fetched when it gave the log, too_large when the log ran past the most komrad keeps, missing when it never had one ready, expired when it no longer keeps it, failed when asking went wrong."
  }
  column "text" {
    null    = false
    type    = text
    default = ""
    comment = "The whole log, cleaned of timestamps and escape codes, empty unless it was fetched."
  }
  column "line_count" {
    null    = false
    type    = integer
    default = 0
    comment = "How many lines the log holds, zero unless it was fetched."
  }
  column "error_count" {
    null    = false
    type    = integer
    default = 0
    comment = "How many of the log's lines the job marked as errors."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "pull_request_check_logs_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  unique "pull_request_check_logs_check_id_key" {
    columns = [column.engine_id, column.check_id]
  }
  foreign_key "pull_request_check_logs_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "pull_request_check_logs_check_id_fkey" {
    columns     = [column.engine_id, column.check_id]
    ref_columns = [table.pull_request_checks.column.engine_id, table.pull_request_checks.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  check "pull_request_check_logs_outcome_known" {
    expr = "outcome IN ('fetched', 'too_large', 'missing', 'expired', 'failed')"
  }
}

table "pull_request_check_log_steps" {
  schema  = schema.public
  comment = "Where one step of a check run sits in the check's log, as the steps stood when the log was fetched."
  column "id" {
    null    = false
    type    = uuid
    default = sql("uuidv7()")
  }
  column "log_id" {
    null    = false
    type    = uuid
    comment = "The log the step sits in."
  }
  column "number" {
    null    = false
    type    = integer
    comment = "Where the step falls in its check run, counting from one."
  }
  column "name" {
    null    = false
    type    = text
    comment = "The step's name when the log was fetched."
  }
  column "conclusion" {
    null    = false
    type    = text
    default = ""
    comment = "How the step ended when the log was fetched, in the forge's own word lowercased."
  }
  column "first_line" {
    null    = false
    type    = integer
    comment = "The log line the step starts on, counting from one."
  }
  column "line_count" {
    null    = false
    type    = integer
    comment = "How many lines of the log the step printed."
  }
  column "error_count" {
    null    = false
    type    = integer
    default = 0
    comment = "How many of the step's lines the job marked as errors."
  }
  column "engine_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "pull_request_check_log_steps_engine_id_id_key" {
    columns = [column.engine_id, column.id]
  }
  unique "pull_request_check_log_steps_number_key" {
    columns = [column.engine_id, column.log_id, column.number]
  }
  foreign_key "pull_request_check_log_steps_engine_id_fkey" {
    columns     = [column.engine_id]
    ref_columns = [table.engines.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "pull_request_check_log_steps_log_id_fkey" {
    columns     = [column.engine_id, column.log_id]
    ref_columns = [table.pull_request_check_logs.column.engine_id, table.pull_request_check_logs.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
}

table "forge_review_threads" {
  schema  = schema.public
  comment = "A review thread as the forge holds it on a pull request a live agent is linked to, which the comment sync owns, overwrites and mirrors into the review tables. The reader role has no grant on it."
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
  comment = "One comment in a forge review thread, which the comment sync owns, overwrites and mirrors into the review tables. The reader role has no grant on it."
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
  column "trusted" {
    null    = false
    type    = boolean
    default = false
    comment = "Whether the forge says the author may write to the repository, which releases their comments to the agent without the person sending them."
  }
  column "forge_review_id" {
    null    = false
    type    = text
    default = ""
    comment = "The forge's identifier for the review the comment was submitted in, empty when it names none."
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
