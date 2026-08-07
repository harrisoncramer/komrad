## weave cookbook

Install a workflow, its actions, and its schedule from a packaged recipe

### Synopsis

A cookbook is a directory holding a cookbook.toml manifest and the action scripts
it names. Installing one writes those scripts into your actions directory and calls
the same create_workflow endpoint the panel's w key and the MCP tools already call,
and create_schedule too when the cookbook arms one, so installing a cookbook is not a
new code path.

weave ships a few built in; list shows those plus anything in your own directory,
configured under [cookbooks] in your config.

### Options

```
  -h, --help   help for cookbook
```

### Options inherited from parent commands

```
      --config stringArray   path to a config TOML, repeatable, later files win over earlier ones
```

### SEE ALSO

* [weave](weave.md)	 - tmux panel and CLI for background coding agents
* [weave cookbook install](weave_cookbook_install.md)	 - Install a cookbook's actions, workflow, and schedule
* [weave cookbook list](weave_cookbook_list.md)	 - List the available cookbooks, built in and your own

