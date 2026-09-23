table "worktree_sizes" {
  schema  = schema.public
  comment = "The disk one worktree directory takes, as the size loop or get_worktree last measured it. Keyed by the directory rather than an agent, since a directory outlives its row and one nobody answers for is what the worktrees screen is for."
  column "dir" {
    null = false
    type = text
  }
  column "size_bytes" {
    null    = false
    type    = bigint
    default = 0
    comment = "Blocks taken rather than length, a hard linked file counted once. Zero while error is set."
  }
  column "measured_at" {
    null    = false
    type    = timestamptz
    comment = "When the walk last finished, whether or not it succeeded, so a failed directory is not walked again on every pass."
  }
  column "error" {
    null    = true
    type    = text
    comment = "Why the last walk failed, null when it did not."
  }
  column "tenant_id" {
    null    = false
    type    = uuid
    default = sql("(current_setting('weave.tenant'::text, true))::uuid")
    comment = "The tenant that owns this row, filled from the connection's weave.tenant setting."
  }
  primary_key {
    columns = [column.dir]
  }
  foreign_key "worktree_sizes_tenant_id_fkey" {
    columns     = [column.tenant_id]
    ref_columns = [table.tenants.column.id]
    on_delete   = RESTRICT
    on_update   = NO_ACTION
  }
}
