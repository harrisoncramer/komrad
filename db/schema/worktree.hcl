table "worktrees" {
  schema  = schema.public
  comment = "One directory under the worktree directories as the filesystem holds it, written by the size loop. Keyed by the directory rather than an agent, since a directory outlives its row and one nobody answers for is what the worktrees screen is for. A directory a delete has claimed has no row."
  column "dir" {
    null = false
    type = text
  }
  column "repo_dir" {
    null    = false
    type    = text
    default = ""
    comment = "The repository the directory was cut from, read off its .git file, empty when there is no such file."
  }
  column "branch" {
    null    = false
    type    = text
    default = ""
    comment = "What the gitdir's HEAD names, empty when it is detached or cannot be read."
  }
  column "readable" {
    null    = false
    type    = boolean
    default = false
    comment = "Whether the gitdir the .git file names is still there."
  }
  column "mtime" {
    null    = true
    type    = timestamptz
    comment = "When git last wrote the directory's .git file, else the directory's own modification time."
  }
  column "size_bytes" {
    null    = false
    type    = bigint
    default = 0
    comment = "Blocks taken rather than length, a hard linked file counted once. Zero until measured and while size_error is set."
  }
  column "measured_at" {
    null    = true
    type    = timestamptz
    comment = "When the walk last finished, whether or not it succeeded, null until the first walk."
  }
  column "size_error" {
    null    = true
    type    = text
    comment = "Why the last walk failed, null when it did not."
  }
  column "tenant_id" {
    null    = false
    type    = uuid
    comment = "The engine that owns this row, stamped from the login that wrote it."
  }
  primary_key {
    columns = [column.tenant_id, column.dir]
  }
  foreign_key "worktrees_tenant_id_fkey" {
    columns     = [column.tenant_id]
    ref_columns = [table.tenants.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
}
