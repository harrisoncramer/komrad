# agentslinter

This module contains weave' custom linter plugin for golangci-lint.

The plugin contains a set of analyzers used to run static analysis against the
source code and flag issues.

The linter is compiled into a self-contained golangci-lint binary, analogous to
custom linters in other ecosystems such as ESLint.

## Analyzers

`forbid_getenv` flags direct `os.Getenv` and `os.LookupEnv` calls so
configuration flows through the config package instead of bare os calls.

`forbid_nil_nil` flags a `return nil, nil` from a function whose last result is an
error and which returns a pointer alongside it, since the caller is handed nothing
and told nothing about why. A sentinel error the caller can match with `errors.Is`
says which of the two happened. It is held to pointers, where nil carries no meaning
of its own, rather than to a nil map or slice that already reads as empty.

`comment_length` flags any comment that runs to more than one sentence, so a
comment says one thing rather than narrating the change that made it. A free
paragraph standing on its own in a file may run long if it sets that reasoning
aside behind an `Explainer:` prefix. A doc comment on a declaration is held to its
one sentence whatever it starts with, and using the prefix there is its own
finding.

The package comment is the exception and has no length limit at all. It is the one
place a package gets to explain what it is for, and it is what a reader meets first
on pkg.go.dev, so it is meant to run to paragraphs. For the same reason it may not
use the `Explainer:` prefix, which would be published as part of the documentation
rather than read as a marker. Every non-main package has to have one, which
staticcheck's `ST1000` enforces. Where the comment outgrows the top of the file it
lives in, move it to a `doc.go` holding nothing else, as `internal/agents` does.

Whole files are left alone by listing path patterns under `exclude`, which are
unanchored regexps matched against the file path.

`context_timeout` flags a `context.Background` or `context.TODO` that is not immediately
bounded by `context.WithTimeout`, and a timeout written inline rather than named by a
constant, `time.Minute` included. It applies only to files matching the unanchored path
regexps under `include`, since a background context is the right answer nearly everywhere
else, and an empty `include` fails the analyzer rather than reading as on while checking
nothing. It is pointed at the panel, which reaches the service over HTTP and would otherwise
draw a frame that never returns.

`comment_prefix` checks that a doc comment on an exported function or method
begins with the identifier name, so an LSP rename keeps the comment in sync.
With `require: true` the comment has to be there at all. In a test file the rule
covers the tests themselves, named `Test`, `Benchmark`, `Fuzz` or `Example`, and
leaves alone the exported stubs a fake needs to satisfy an interface.

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
