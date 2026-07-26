// Atlas config. The HCL under ./schema is the desired state; Atlas diffs it
// against the versioned migrations in ./migrations and writes what is missing.
//
// Atlas authors migrations here and never applies them. Applying is the
// binary's job, through its own embedded runner, so one thing writes the schema
// and one ledger records what it wrote. Were both to apply, Atlas's revision
// table and the binary's would each hold part of the history and neither would
// agree the schema was current.
//
// There is no url for that reason: `migrate diff` only needs the dev database,
// a throwaway container Atlas starts and drops itself, so authoring a migration
// needs Docker but no running development database.
env "local" {
  src = "file://schema"
  dev = "docker://postgres/18/dev?search_path=public"
  migration {
    dir = "file://migrations"
  }
}
