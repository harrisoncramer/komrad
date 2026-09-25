---
title: From proto to every surface
description: One RPC declaration becomes a handler, a CLI command, an MCP tool and a reference page.
group: architecture
order: 15
---

Every endpoint in komrad is declared once, as an RPC in a protobuf file. Everything else is derived from it.

## The pipeline

```
  <pkg>.proto  with  surfaces { cli, mcp, name, ... }
       |
       |  buf lint
       v
  buf generate
       |--> types            (protoc-gen-go)
       |--> routes, client   (connect-go)
       |--> strict handler   (komrad's plugin)
       v
  read at startup by internal/rpcapi
       |--> server handlers
       |--> komrad rpc commands
       |--> MCP tools
       |--> docs
```

Each RPC carries one annotation, `surfaces`. It says whether the RPC gets a CLI command, whether it becomes an MCP tool, what it is called, and which agent roles may use it. If a surface is left off, it must say why. For instance, `EmptyArchive` is a CLI command but not an MCP tool, because it cannot be undone.

## Lint

Buf lint runs with a plugin we wrote. It checks shape, not style. A method named `List` must declare no side effects. A method named `Archive` must be idempotent. An id field must refuse empty. And every method must have a `surfaces` annotation that is complete. That last rule is what makes the promise in [How it works](../how-it-works/) true. A method cannot be added to one surface and forgotten on the other, because the file will not lint.

## Generation

Three generators run. Two are the standard protobuf and Connect ones. The third is ours. It writes a strict interface whose methods return a typed failure instead of an `error`, and an adapter that turns the failure into a Connect code. A handler implements the strict interface, so its body is one call. The mapping from failure to code lives in one place.

## The server

A request passes through a few interceptors before it reaches a handler. A guard refuses any Host header that is not loopback, which stops a web page on your machine from reaching the API. A recovery layer turns a panic into an error. The JSON codec refuses unknown fields instead of dropping them. Validation runs the rules declared in the proto. Then the handler makes one call into a service.

The caller is carried in two headers naming the agent and its session. That is how a handler knows an agent may only archive its own children.

## CLI and MCP

No CLI or MCP code is generated. At startup, the service walks the compiled proto descriptors and builds one `komrad rpc` subcommand per method, with a flag per field. It builds one MCP server per agent role, holding only the tools that role may use. A tool call is a JSON post to the same route the panel uses, so it arrives at the handler looking exactly like a keypress.

## Docs and drift

`cmd/gen-docs` walks the same descriptors and writes the JSON that the [API reference](../../api/) renders. The generated files are committed. CI regenerates everything and fails if anything changed. So a proto edit without a regenerate fails the build, and the docs never lag the code.
