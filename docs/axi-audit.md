# AXI audit: llm-data against the ten principles

A hand audit of `llm-data` at `e32edfe` against the ten principles on https://axi.md, for epic 03 T1. `axi-axi` is not installed on the build host. Every probe ran against `./build/llm-data` from `make build`.

**Summary:** met: 1 (its MCP half since T6), 2 and 10. Partly met: 6. Not met: 3, 4, 5, 7, 8, 9. Three gaps are closed by **none of T2-T6**: 3 (truncation), 4 (totals), and the residue of 5 (an explicit zero count). A fourth sits under 6 (`yaml push` is not idempotent).

| # | Principle (axi.md) | Verdict | Evidence | Closed by |
|---|---|---|---|---|
| 1 | Token-efficient output (TOON) | **Met** (MCP since T6) | `--axi` is a persistent root flag (`internal/data/commands/root.go:100`). Probed with `--axi`, `grep`, `multigrep`, `stats`, `tree`, `git-changes`, `plan-type`, `repo-root`, `detect`, `count`, `math`, `catfiles` and `yaml` all print TOON. Only `math` (`axi_coverage_test.go`) and three `yaml` cases (`axi_review_test.go`) have a test that asserts it. At audit time the MCP server sent `--json --min` unless `LLM_DATA_MCP_AXI=true` or `format: "axi"`. Since T6 it sends `--axi` by default and `--json --min` only for `format: "json"` or `LLM_DATA_MCP_AXI=false` (`internal/data/mcpserver/handlers.go:98,179-183`), and `tests/mcp_integration` asserts both over stdio. | MCP half: T6 (done) |
| 2 | Minimal default schemas (3-4 fields per item) | **Met** | `grep` rows are `{file,text}` (`grep.go:27-31`), `multigrep` rows are `{content,file,line}`, `tree` items are `{name,path,type}`, `stats` rows are `{count,extension,size}`. | — |
| 3 | Content truncation with a size hint and `--full` | **Not met** | `grep` emits each matching line whole, in every mode. No command has a `--full` flag. `multigrep` cuts content at 80 characters only in plain mode (`multigrep.go:237,244`), and `git-context` cuts the diff at 50,000 bytes with a hint but no way to get the rest (`git_context.go:172`). | **None of T2-T6** (gap) |
| 4 | Pre-computed aggregates, always a total | **Not met** | `grep` reports no count at all. `multigrep` does carry `total_matches` in `--json`/`--axi` (5 for `package` in `root.go`), but the field is `omitempty` (`multigrep.go:55`), so a search with no matches drops it. A keyword with no matches drops its `match_count` too: it prints `zzqq1:` with nothing under it. `stats` does carry totals. | **None of T2-T6** (gap) |
| 5 | Definitive empty states | **Not met** | `grep zzqqxxnomatch internal/data/commands/math.go`, a real file, prints nothing in plain mode, `{"pattern": "..."}` in `--json` and `pattern: ...` in `--axi`, exit 0. `grep foo /no/such/path` prints **the same thing**, exit 0. | Missing path: T2. `help` on zero matches: T4. An explicit `0` count: **none** (gap, same as 4) |
| 6 | Structured errors on stdout, exit 0/1, idempotent, never prompt | **Partly met** | See 6a-6d below. | 6a: T2. 6b: T7 states the exception. 6d: **none** (gap) |
| 7 | Ambient context (setup command, Agent Skill) | **Not met** | `llm-data setup` gives `Error: unknown command "setup"`, exit 2. | T5 |
| 8 | Content first (a no-argument run shows live data, path as `~`) | **Not met** | A run with no arguments prints the cobra help text and exits 0. | T3 |
| 9 | Contextual disclosure (`help[]` next steps) | **Not met** | No output in any mode carries a `help` list. A grep for `"help"` in `internal/` and `pkg/` finds no emitter. | T4 (home view, zero-match `grep` and `multigrep` only) |
| 10 | Consistent `--help` on every subcommand | **Met** | `--help` exits 0 for all 21 subcommands and all 10 `yaml` subcommands (`delete`, `get`, `init`, `list`, `multiget`, `multiset`, `pop`, `push`, `set`, `validate`). | — |

## Principle 6 in detail

- **6a. A missing path is reported as zero matches.** `grep foo /no/such/path`, in plain, `--json` and `--axi`, prints the same output as a real zero-match search and exits 0 (see 5). This is the AGENTS.md headline bug: nothing may report success when it did not succeed. Closed by **T2**.
- **Met: runtime errors are structured on stdout.** `math '1/('` with `--json` prints `{"error": true, "message": ...}` on stdout. With `--axi` it prints `error: true` / `message: ...` on stdout. Both exit 1 with nothing on stderr. `catfiles /no/such --axi` and `yaml get --file /no/such.yaml --json` behave the same way. In plain mode the error goes to stderr as `Error: ...`, exit 1.
- **6b. Usage errors exit 2, on stderr, even in `--axi`.** `grep --bogus --axi` prints `Error: unknown flag: --bogus` on stderr (29 bytes, 0 on stdout) and exits 2. axi.md asks for exit codes 0/1 and errors on stdout. Exit 2 for a malformed invocation is the go-axi convention (`goaxi.ExitUsage`). The README must state it as an exception: **T7**.
- **Met: never prompts.** Nothing reads a terminal. The one stdin reader, `readFromStdin` (`yaml_helpers.go:909-921`), refuses a TTY with an error instead of waiting.
- **6d. `yaml push` is not idempotent.** Running `yaml push --file c.yaml a.l x` twice leaves `[x, x]`, and both runs report success with exit 0: `status: pushed` with `--axi` (the mode probed), `PUSHED: x to a.l` in plain mode. `yaml init` (`CREATED`, then `EXISTS`) and `yaml set` (byte-identical file) are idempotent. **No task in T2-T6 closes this.** Push is append by name, so the fix may be a README exception rather than a code change.

## Not probed

`git-changes`, `git-context` and `repo-root` were not probed for empty states outside a repository.
