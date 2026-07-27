# agentslinter

This module contains claude-agents' custom linter plugin for golangci-lint.

The plugin contains a set of analyzers used to run static analysis against the
source code and flag issues.

The linter is compiled into a self-contained golangci-lint binary, analogous to
custom linters in other ecosystems such as ESLint.

## Analyzers

`forbid_getenv` flags direct `os.Getenv` and `os.LookupEnv` calls so
configuration flows through the config package instead of bare os calls.

`comment_prefix` checks that a doc comment on an exported function or method
begins with the identifier name, so an LSP rename keeps the comment in sync. It
does not require exported functions to be documented.

Each analyzer reads its settings from the `custom.agentslinter.settings` block in
`.golangci.yaml` and can be turned off individually with `skip: true`.

## Development

After adding or changing an analyzer, rebuild the binary:

```bash
mise run go:build-linter
```

Behind the scenes this runs `golangci-lint custom`, which fetches the core linter source and bundles in the plugins listed in `.custom-gcl.yml`.

## Testing

Analyzers are tested with the `analysistest` package. Test data lives under
`testdata/src/<pkg>`, with expectations encoded as `// want` comments in the
source. Run the tests from this module:

```bash
cd tools/agentslinter && go test ./...
```
