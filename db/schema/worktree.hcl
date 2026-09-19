table "worktree_sizes" {
  schema  = schema.public
  comment = "How many bytes each worktree directory held when the service last counted it. A row is dropped when its directory goes."
  column "dir" {
    null    = false
    type    = text
    comment = "The directory, absolute, as the worktree listing spells it."
  }
  column "size_bytes" {
    null    = false
    type    = bigint
    comment = "The sum of the sizes of every file under the directory, following no symlinks."
  }
  column "counted_at" {
    null    = false
    type    = timestamptz
    comment = "When the count finished."
  }
  column "entries" {
    null    = false
    type    = jsonb
    default = "[]"
    comment = "The directory's top-level entries as the count passed over them, each with its path, its bytes and whether it is a directory, biggest first."
  }
  primary_key {
    columns = [column.dir]
  }
}
