## weave engine ensure

Create an engine's identity and logins, or reset their password

### Synopsis

Create an engine's identity and logins, or reset their password.

An engine is one weave service on one machine. Everything it writes belongs to it, and
Postgres keeps every other engine's rows out of its reach, because it connects as a login
of its own that the database maps to its identity. This creates that identity if no engine
has the name yet, a login for it and a read-only login for its MCP query tools, and the
schema its job queue lives in, connecting as owner_database_url.

Both logins take the password in WEAVE_ENGINE_PASSWORD, or a fresh one when that is unset,
and running it again resets them to it. It prints the database_url the engine connects with.
Naming an engine that already exists, such as the default one every row written before
engines had logins belongs to, hands it new logins and keeps its rows.

```
weave engine ensure NAME [flags]
```

### Options

```
  -h, --help   help for ensure
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
      --engine string        engine to reach, named after a file in the config's engines directory, layered over the config
```

### SEE ALSO

* [weave engine](weave_engine.md)	 - Set up the engines that share a database

