# Vision

`llm-data-axi` exists so that an agent can answer routine questions about files, projects and config without spending a model on them.
It serves agents first, and the people who run them second.
It turns the questions an agent asks constantly into small, deterministic commands with token-dense output.
It owns exactly one thing: cheap, deterministic, local facts and config for a working directory.

## Nothing reports success when it did not succeed

This rule sits above every other rule here.
A `grep` that fails on a bad path and a `grep` that finds nothing are never reported the same way.
An encode that produces no output is an error, not an exit 0.
A command that returns a list always returns the key, even when the list is empty.
A test asserts the side effect, not the exit code.

## Deterministic

The same input returns the same output.
A command that can be answered with code is answered with code.
No command calls a model or the network.
Web and model work belongs to `llm-extract-axi`.

## The CLI is the product

`llm-data` is the primary surface.
The MCP server shells out to the CLI, is a fallback for hosts that cannot run one, and holds no logic of its own.
Output follows the AXI principles, and TOON through `go-axi` is the format given to a model.
A client that asks for TOON gets TOON.

## Small and separate

Installing this tool never pulls in sqlite, qdrant or tree-sitter.
A heavy dependency is a reason for a different tool.
Semantic search belongs to `llm-semantic-axi`, and file reads and writes belong to `llm-filesystem-axi`.
Workflow state does not belong here.
Commands that duplicate `llm-filesystem-axi` are temporary, and they retire once that tool respects `.gitignore`.
The split from `llm-tools` is not undone.

## Scope

It is not a file manager, a search index, a workflow engine or a project planner.
Git commands only read, because a tool of facts does not act.
A new command earns its place by replacing a question agents ask often, and `math` stays for that reason.
`yaml` stays, because agents edit config constantly and a separate install costs more than a wider identity.
Plans and debt live in a private planning repo.

A change aligns when it answers a frequent agent question with code, in fewer tokens, with a truthful empty state.
A change should be resisted when it calls a model, uses the network, adds a heavy dependency, or moves work here that belongs to a sibling tool.
