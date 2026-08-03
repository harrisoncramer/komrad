## weave migrate

Apply any pending database migrations

### Synopsis

Apply any pending database migrations against the configured database.

The service migrates its own schema on start, so this is for migrating without
starting one: a fresh database, or a deploy step. It also applies River's queue
migrations, which the service only reaches because it is the process that runs the
queue, so asking on purpose has to ask for both.

It is safe to run against a database that is already current; it says so and exits
zero.

```
weave migrate [flags]
```

### Options

```
  -h, --help   help for migrate
```

### Options inherited from parent commands

```
      --config string   path to config TOML
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

