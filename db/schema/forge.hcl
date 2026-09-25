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
  column "tenant_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  foreign_key "forge_connections_tenant_id_fkey" {
    columns     = [column.tenant_id]
    ref_columns = [table.tenants.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  unique "forge_connections_host_key" {
    columns = [column.tenant_id, column.host]
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
  column "decision_changed_at" {
    null    = true
    type    = timestamptz
    comment = "The changed_at the review decision was read for, stale once changed_at has moved past it."
  }
  column "checks" {
    null    = true
    type    = text
    comment = "Where the forge says the checks on the head commit stand, in komrad's own words: pending, passing or failing, null while it reports none. A check finishing does not move changed_at, so the checks are kept fresh by checks_sha and checks_read_at rather than by the change stamp."
  }
  column "checks_sha" {
    null    = true
    type    = text
    comment = "The head commit the checks were read for, stale once head_sha has moved past it."
  }
  column "checks_read_at" {
    null    = true
    type    = timestamptz
    comment = "When the checks and mergeability were last read, which is what paces every re-read."
  }
  column "mergeability" {
    null    = true
    type    = text
    comment = "Where the forge says the pull request stands against its base and its branch rules, in komrad's own words: clean, unstable, blocked, behind or conflicting, null while the forge has not computed it. Read with the checks and kept fresh the same way."
  }
  column "tenant_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.id]
  }
  unique "pull_requests_tenant_id_id_key" {
    columns = [column.tenant_id, column.id]
  }
  foreign_key "pull_requests_tenant_id_fkey" {
    columns     = [column.tenant_id]
    ref_columns = [table.tenants.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  unique "pull_requests_host_provider_id_key" {
    columns = [column.tenant_id, column.host, column.provider_id]
  }
  unique "pull_requests_url_key" {
    columns = [column.tenant_id, column.url]
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
  column "tenant_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.agent_id, column.pull_request_id]
  }
  foreign_key "agent_pull_requests_tenant_id_fkey" {
    columns     = [column.tenant_id]
    ref_columns = [table.tenants.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
  foreign_key "agent_pull_requests_agent_id_fkey" {
    columns     = [column.tenant_id, column.agent_id]
    ref_columns = [table.agents.column.tenant_id, table.agents.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  foreign_key "agent_pull_requests_pull_request_id_fkey" {
    columns     = [column.tenant_id, column.pull_request_id]
    ref_columns = [table.pull_requests.column.tenant_id, table.pull_requests.column.id]
    on_delete   = CASCADE
    on_update   = NO_ACTION
  }
  index "agent_pull_requests_pull_request_id" {
    columns = [column.pull_request_id]
  }
}

table "pull_request_syncs" {
  schema  = schema.public
  comment = "Where the pull request sync is for one repository: the high-water mark the changes have been read through, the first sweep in progress if one is, and what the last pass cost."
  column "repository_url" {
    null = false
    type = text
  }
  column "updated_through" {
    null    = true
    type    = timestamptz
    comment = "The newest forge updated_at the changes have been read through, by the first sweep and then by every search since. Null until the first sweep has completed."
  }
  column "listed_at" {
    null    = true
    type    = timestamptz
    comment = "The last time page one of the repository's own listing was read and written, which is the clock for the hourly listing behind the search."
  }
  column "sweep_cursor" {
    null    = true
    type    = text
    comment = "Where the first sweep in progress reads its next page from, empty for page one and null between sweeps."
  }
  column "sweep_newest" {
    null    = true
    type    = timestamptz
    comment = "The moment the sweep in progress covers up to, when its page one was read or the newest row's stamp when the forge's clock is ahead, which becomes updated_through when it completes."
  }
  column "read_at" {
    null    = true
    type    = timestamptz
    comment = "The last time any request was made for this repository, whatever it answered."
  }
  column "calls" {
    null    = false
    type    = integer
    default = 0
    comment = "How many requests the most recent pass made for this repository."
  }
  column "tenant_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.tenant_id, column.repository_url]
  }
  foreign_key "pull_request_syncs_tenant_id_fkey" {
    columns     = [column.tenant_id]
    ref_columns = [table.tenants.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
}

table "branch_scans" {
  schema  = schema.public
  comment = "A branch komrad has listed the pull requests of on purpose, once, which is what finds a pull request older than the sync's horizon on a branch an agent adopted."
  column "repository_url" {
    null = false
    type = text
  }
  column "branch" {
    null = false
    type = text
  }
  column "next_cursor" {
    null    = false
    type    = text
    default = ""
    comment = "Where the listing reads its next page from, empty for the first."
  }
  column "completed_at" {
    null    = true
    type    = timestamptz
    comment = "When the listing answered its last page, after which the branch is never scanned again."
  }
  column "tenant_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.tenant_id, column.repository_url, column.branch]
  }
  foreign_key "branch_scans_tenant_id_fkey" {
    columns     = [column.tenant_id]
    ref_columns = [table.tenants.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
}
