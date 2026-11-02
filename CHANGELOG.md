# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- The no-network guard (`internal/noweb_guard_test.go`) now also scans `cmd/`, so a `net/http`, `net` or `goquery` import in either binary fails the build.

### Removed

- The six extra `yaml` subcommands `init`, `list`, `delete`, `validate`, `push` and `pop`, with their templates and helpers. `yaml` now has only `get`, `set`, `multiget` and `multiset`; the removed names are rejected as unknown commands.

- The `extract-relevant` and `extract-links` CLI commands and the `extract_relevant` and `extract_links` MCP tools (with their `llmCommands` map and `LLMTimeout`) are gone; the server now advertises 24 tools and rejects calls to the removed ones as unknown. They move to [llm-extract-axi](https://github.com/samestrin/llm-extract-axi), which ships them as `llm-extract extract-relevant` and `llm-extract extract-links` (MCP: `extract_relevant` and `extract_links` on `llm-extract-mcp`). Nothing left in this repository makes a network call: `internal/noweb_guard_test.go` fails the build if a non-test file under `internal/` or `pkg/` imports `net/http`, `net` or `goquery`.
- The `pkg/llmapi` package (client, concurrency, config, response, streaming) with its tests, plus the `golang.org/x/sync` dependency it was the only consumer of (`goquery`, used by the extract commands above, is gone too). It moves to [llm-extract-axi](https://github.com/samestrin/llm-extract-axi).

### Added

- Standalone repository, extracted from [samestrin/llm-tools](https://github.com/samestrin/llm-tools). Twenty-six tools travelled across 25 source files, together with `pkg/output`, `internal/support/gitignore` and `internal/support/utils`, renamed to `internal/data/*`; the two LLM commands and the LLM client package they used have since moved on (see Removed), leaving 24.
- `Makefile` with `build`, `build-all`, `test`, `test-race`, `test-cover`, `lint`, `vet`, `fmt`, `install`, `clean` and `hooks` targets.
- CI: `lint` → `test -race` → `coverage`, chained by `needs:`, plus a five-platform cross-compile matrix with a 20MB binary size gate, and a tag-triggered release workflow.
- `.golangci.yml` matching the selection used across `samestrin/atcr` and `samestrin/go-axi`.
- **`help` next steps in `--json` and `--axi`.** The home view, a `grep` that matched nothing and a `multigrep` where no keyword matched carry a `help` list of suggested next commands; a zero-match run leaves out flags the caller already passed. A run with matches, and plain-text output, carry none.
- `tests/mcp_integration` drives the built `llm-data-mcp` over real stdio — JSON-RPC framing, the SDK registration loop and the exec of the CLI — which no in-package test can reach.

### Changed

- **A bare `llm-data` run shows a home view instead of cobra help.** It prints two lines, `bin:` with the executable path (home directory written as `~`) and `description:` with one sentence, and honours `--json` and `--axi`. `--help` still prints help, and an unknown subcommand still fails with exit 2. If the executable path cannot be resolved the run fails rather than printing an empty `bin:`.
- go-axi moves from **v0.1.2** to **v0.3.0**.
- Binary renamed `llm-support` → `llm-data`; MCP tool prefix `llm_support_` → `llm_data_`; debug variable `LLM_SUPPORT_MCP_DEBUG` → `LLM_DATA_MCP_DEBUG`.
- **The MCP server now asks the CLI for TOON (`--axi`) by default.** Pass `"format": "json"` on a call, or start the server with `LLM_DATA_MCP_AXI=false`, to get `--json --min`; an unknown `format` value is now an error (`runtime` keeps its own `format`, the time format).
- Thirteen direct dependencies that only the unported commands used are dropped, plus the two the removed LLM code was the only consumer of. Seven direct requires remain.
- The CLI version is stamped by `-X` onto `commands.Version` at build time instead of being hardcoded.

### Fixed

- **`grep` treated a missing path as "no matches".** A path that failed `os.Stat` was skipped with `continue`, so `llm-data grep foo /nonexistent --axi` printed `pattern: foo` and exited 0, and `--json` printed `{"pattern":"foo"}`. It now fails with `path not found: <path>` and exit 1 in plain, `--json` and `--axi` modes, including when the missing path sits beside real ones.
- **`--axi` was unreachable through MCP.** `ExecuteHandler` appended `--json --min` to every command unconditionally and no handler ever passed `--axi`, so an MCP client could not obtain TOON no matter what the CLI supported. The server now sends exactly one encoding flag, selected by `LLM_DATA_MCP_AXI` or by a per-call `"format": "axi"` argument. Pinned end to end over stdio.
- **A data race in `BatchProcessor.ProcessItemsWithProgress`.** The completion counter was incremented from inside each worker goroutine with no lock. Measured under `-race` across 32 items at concurrency 8: the final progress report read "29 of 32" and three intermediate values were reported twice. The counter and the callback now run under one mutex, so the callback also sees a strictly increasing count.
- **The MCP server hardcoded `/usr/local/bin/llm-support`** with no lookup, so it refused to start for anyone who installed the CLI elsewhere, and the repo's own end-to-end tests could not run without installing first. `BinaryPath` now resolves from `PATH`, falls back to the install location, and honours `LLM_DATA_BINARY`.
- **A help-text promise that nothing kept.** `extract-relevant --help` advertised a `.planning/.config/openai_api_key` file. No such file was ever read; the LLM client configuration was environment-only. The line was corrected to name the environment variables that actually work; the command itself has since been removed (see Removed).
- **`git-context` listed remotes in a different order on every run.** The list was built by ranging over a map, so a repository with more than one remote printed them in random order. Remotes are now sorted by name, and the `git remote -v` parser is pinned by a table test over captured output.

### Not ported

Thirty-four command files stayed in `llm-tools` because no consumer calls them, and fifteen TD, temp and context tools are migrating to `cadence-axi` instead. Nothing was deleted — `llm-tools` remains live — so porting any of them later is cheap.

### Coverage gate

CI fails below 80% total coverage rather than printing a percentage and moving on. Measured cold and with the LLM environment stripped: **83.9%**.
