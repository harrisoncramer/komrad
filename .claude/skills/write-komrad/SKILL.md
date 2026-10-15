---
name: write-komrad
description: Write anything a person or an agent reads in komrad - proto comments, docs pages, pull request descriptions, commit messages, RFCs, task notes, mail and review replies - in the house style of docs/style-guide.md. Load before writing or rewriting any of these.
---

# Writing for komrad

Read `docs/style-guide.md` in full before you write. It holds the glossary, the general rules, and the rules for proto comments. Where this skill and the guide differ, the guide wins.

## Before you write

Look up each concept you mention in the glossary and use its term. Do not rotate synonyms. If a concept has no entry, use the word the code uses, and add an entry to the glossary in the same change.

## While you write

Write short, plain sentences, one idea each. Name the thing instead of opening with "it". Say what is true and stop. Put names from code in backticks.

For a proto comment, write for a reader on the API reference page who has never seen the source. Say what the field, message or call is from the caller's side. Leave out how komrad does it. Do not restate validation rules. Advice to a model goes in `mcp_guidance`, not `description`.

## Before you finish

Search what you wrote for the words in the guide's "Words not to use" section and for the replaced terms the glossary lists after "Not". Remove each one you find.

After changing a proto, run `mise run proto:generate` and then `mise run gen:docs`, and read the changed text in `docs/src/data/api.json`. That file is what the reference shows.
