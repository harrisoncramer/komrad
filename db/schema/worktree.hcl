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
  primary_key {
    columns = [column.dir]
  }
}
