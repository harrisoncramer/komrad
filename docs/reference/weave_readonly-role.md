## weave readonly-role

Create the least-privilege role the MCP query tools connect as

### Synopsis

Create the least-privilege role the MCP query tools connect as.

The SQL half of the MCP endpoint runs whatever an agent asks it to, so it connects
as a login that can read the application tables and nothing else. That is enforced
by Postgres rather than by inspecting the query. This creates or updates that role
from the user and password in WEAVE_READONLY_DATABASE_URL, using DATABASE_URL to do
the granting.

It refuses when the two URLs name the same login, since then the read-only path would
have the owner's privileges. Run it once when setting a database up. Tables a later
migration adds are covered on their own, and running it again is harmless.

```
weave readonly-role [flags]
```

### Options

```
  -h, --help   help for readonly-role
```

### Options inherited from parent commands

```
      --config string   path to config TOML
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents

