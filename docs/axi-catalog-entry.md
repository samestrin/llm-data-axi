# AXI catalog entry (draft): llm-data

**Status:** draft for v2.0.0, for review before publication to the AXI catalog.

## Name

`llm-data` — a general-purpose data and project CLI built to the [AXI](https://axi.md) principles, with a companion MCP server (`llm-data-mcp`).

## What it does

Eighteen commands for inspecting a codebase and its data (`llm-data --help` lists them all): `tree`, `stats`, `detect`, `discover-tests`, `grep`, `multigrep`, `count`, `catfiles`, `yaml get/set/multiget/multiset`, `math`, `runtime`, `git-changes`, `git-context`, `repo-root`, `setup` and more. Most are read-only; the writers are `yaml set` and `yaml multiset` (the target YAML file, in place), `setup` (agent-integration files inside the project), `template --output` (the rendered file) and `multigrep --output-dir` (one result file per keyword).

Output is token-dense TOON by default. `--json` prints JSON, `--plain` prints plain text, and `--axi` is an accepted alias for the default. `--min` shortens `--json` and `--plain` output and has no effect on TOON; `--plain` combined with `--json` or `--axi` is a usage error (exit 2) naming both flags. Errors go to stdout in the active format, with exit codes 0 (success), 1 (error) and 2 (usage error). A partial result is an error too: `template --strict` with an undefined variable, `catfiles` with a path it cannot read and `multigrep --output-dir` with a keyword file it cannot write each exit 1 and list what failed. The MCP server asks the CLI for TOON by default.

A bare `llm-data` run prints a home view — the binary path, a one-sentence description and the repository it runs in — and `llm-data setup` registers the CLI with Claude Code, Codex and OpenCode in the current project (`--target`, comma-separated or repeated; the default is `claude` alone).

Nothing in the tool makes a network call — a build-time guard (`internal/noweb_guard_test.go`) fails the build if a non-test file imports `net/http`, `net` or `goquery`.

## AXI conformance

The hand audit in [`docs/axi-audit.md`](axi-audit.md) scores every principle as met except two stated, justified remainders (principle 9 is partly met within its stated scope):

- **No session-end lifecycle-capture hook.** `llm-data` keeps no session state — no transcript, no record of files touched — so there is nothing for a session-end hook to capture. The session-start integration (home view as context) ships for all three default app targets.
- **`help[]` next steps are scoped, not universal.** They sit where a result leaves an agent without a next step: the home view, empty search results and truncated search lines. Widening them to every command is deliberately left as future work.

## Token figures

Measured by `go run ./benchmarks` over six read-only commands, each run once with `--json` and once with `--axi`, on this repository and on `spf13/cobra@v1.8.0`. Tokens are approximated as bytes/4; "Saved" is the share of JSON bytes TOON does not spend. Full table: `benchmarks/RESULTS.md`.

- **Total across all twelve runs: 281,592 JSON bytes (~70,398 tokens) against 170,378 TOON bytes (~42,594 tokens) — 39.5% saved.**
- Largest single saving: `stats` on llm-data-axi, 1,007 JSON bytes → 281 TOON bytes (**72.1%** saved); on cobra, 685 → 218 (**68.2%**).
- Largest absolute saving: `grep -n "^func " .` on llm-data-axi, 144,602 JSON bytes (~36,150 tokens) → 91,471 TOON bytes (~22,867 tokens), 36.7% saved.
- `tree --depth 3` on llm-data-axi: 22,028 → 9,250 bytes (58.0% saved); on cobra: 7,824 → 4,129 bytes (47.2%).

`tree`, `stats`, `grep` and `multigrep` print paths relative to the working directory by default in both formats (`--absolute` restores absolute paths), so the figures do not shift with where the repository sits on disk.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-data-axi/main/install.sh | sh
```

Prebuilt binaries for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64` and `windows/amd64` (best effort, no guarantee), checked against the release's `SHA256SUMS.txt` before install. Build from source with `make install`.

## Links

- Repository: https://github.com/samestrin/llm-data-axi
- Extracted from: https://github.com/samestrin/llm-tools
- Benchmark: `benchmarks/RESULTS.md` in the repository
