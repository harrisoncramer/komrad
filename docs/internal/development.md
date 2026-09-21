# Developing weave

The contributor path, which needs Go 1.25.1 or newer. Every task below is a `mise` task in the checkout.

```sh
git clone git@github.com:harrisoncramer/weave.git ~/weave
cd ~/weave
mise install
mise run go:build
mise run docker:up
```

`mise run check:quick` builds everything and tests and lints what your branch touched. `mise run check` is the whole suite.
