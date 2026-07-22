// Atlas config. The HCL under ./schema is the desired state; Atlas diffs it
// against the versioned migrations in ./migrations and applies them to the
// SQLite database at DATABASE_URL.
env "local" {
  src = "file://schema"
  dev = "sqlite://dev?mode=memory"
  url = getenv("DATABASE_URL")
  migration {
    dir = "file://migrations"
  }
}
