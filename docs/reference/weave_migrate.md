## weave migrate

Apply any pending database migrations

### Synopsis

Apply any pending database migrations, connecting as the login that owns the schema.

The service never migrates: an engine connects as a login that cannot change the schema,
and it refuses to start against a database whose migrations do not match its build. So
every upgrade runs this first, with owner_database_url (or WEAVE_OWNER_DATABASE_URL)
naming the owner, which falls back to database_url on a database no engine has joined.

It refuses while another weave is connected and the build holds migrations the database
has not seen, since those engines would stop matching. It is safe to run against a
database that is already current; it says so and exits zero.

```
weave migrate [flags]
```

### Options

```
  -h, --help   help for migrate
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

