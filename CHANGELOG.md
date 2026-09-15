# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Standalone repository, extracted from [samestrin/llm-tools](https://github.com/samestrin/llm-tools). Twenty-six tools travel across 25 source files, together with `pkg/output`, `pkg/llmapi`, `internal/support/gitignore` and `internal/support/utils`, renamed to `internal/data/*`.
- `Makefile` with `build`, `build-all`, `test`, `test-race`, `test-cover`, `lint`, `vet`, `fmt`, `install`, `clean` and `hooks` targets.
- CI: `lint` → `test -race` → `coverage`, chained by `needs:`, plus a five-platform cross-compile matrix with a 20MB binary size gate, and a tag-triggered release workflow.
- `.golangci.yml` matching the selection used across `samestrin/atcr` and `samestrin/go-axi`.
- `tests/mcp_integration` drives the built `llm-data-mcp` over real stdio — JSON-RPC framing, the SDK registration loop and the exec of the CLI — which no in-package test can reach.
- `pkg/llmapi/concurrency.go` gains its first tests. It had none in `llm-tools`.

### Changed

- go-axi moves from **v0.1.2** to **v0.3.0**.
- Binary renamed `llm-support` → `llm-data`; MCP tool prefix `llm_support_` → `llm_data_`; debug variable `LLM_SUPPORT_MCP_DEBUG` → `LLM_DATA_MCP_DEBUG`.
- Thirteen direct dependencies that only the unported commands used are dropped. Ten direct requires remain.
- The CLI version is stamped by `-X` onto `commands.Version` at build time instead of being hardcoded.

### Fixed

- **`--axi` was unreachable through MCP.** `ExecuteHandler` appended `--json --min` to every command unconditionally and no handler ever passed `--axi`, so an MCP client could not obtain TOON no matter what the CLI supported. The server now sends exactly one encoding flag, selected by `LLM_DATA_MCP_AXI` or by a per-call `"format": "axi"` argument. Pinned end to end over stdio.
- **The two commands that call an LLM had the shortest timeout.** The `llmCommands` map named `complete` and `foreach`, neither of which travelled, and omitted `extract_relevant` and `extract_links` — the only two tools here that make a network LLM call. They were capped at 60 seconds instead of 300.
- **A data race in `BatchProcessor.ProcessItemsWithProgress`.** The completion counter was incremented from inside each worker goroutine with no lock. Measured under `-race` across 32 items at concurrency 8: the final progress report read "29 of 32" and three intermediate values were reported twice. The counter and the callback now run under one mutex, so the callback also sees a strictly increasing count.
- **The MCP server hardcoded `/usr/local/bin/llm-support`** with no lookup, so it refused to start for anyone who installed the CLI elsewhere, and the repo's own end-to-end tests could not run without installing first. `BinaryPath` now resolves from `PATH`, falls back to the install location, and honours `LLM_DATA_BINARY`.
- **A help-text promise that nothing kept.** `extract-relevant --help` advertised a `.planning/.config/openai_api_key` file. No such file is ever read; `pkg/llmapi/config.go` is environment-only. The line now names the environment variables that actually work.

### Not ported

Thirty-four command files stayed in `llm-tools` because no consumer calls them, and fifteen TD, temp and context tools are migrating to `cadence-axi` instead. Nothing was deleted — `llm-tools` remains live — so porting any of them later is cheap.

### Coverage gate

CI fails below 80% total coverage rather than printing a percentage and moving on. Measured cold and with the LLM environment stripped: **83.9%**.
