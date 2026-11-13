# AXI catalog entry (draft): llm-data

**Status:** draft for v1.0.0, for review before publication to the AXI catalog.

## Name

`llm-data` — a general-purpose data and project CLI built to the [AXI](https://axi.md) principles, with a companion MCP server (`llm-data-mcp`).

## What it does

Twenty-four tools for inspecting a codebase and its data: `tree`, `stats`, `detect`, `discover-tests`, `grep`, `multigrep`, `count`, `catfiles`, `yaml get/set/multiget/multiset`, `math`, `runtime`, `git-changes`, `git-context`, `repo-root`, and more. All read-only except `yaml set` and `yaml multiset`, which write the target YAML file in place. Every command honours `--json` and `--axi` (TOON); the MCP server asks the CLI for TOON by default. A bare `llm-data` run prints a home view, and `llm-data setup` registers the CLI with Claude Code in the current project.

Nothing in the tool makes a network call — a build-time guard (`internal/noweb_guard_test.go`) fails the build if a non-test file imports `net/http`, `net` or `goquery`.

## Token figures

Measured by `go run ./benchmarks` over six read-only commands, each run once with `--json` and once with `--axi`, on this repository and on `spf13/cobra@v1.8.0`. Tokens are approximated as bytes/4; "Saved" is the share of JSON bytes TOON does not spend. Full table: `benchmarks/RESULTS.md`.

- **Total across all twelve runs: 294,538 JSON bytes (~73,634 tokens) against 198,573 TOON bytes (~49,643 tokens) — 32.6% saved.**
- Largest single saving: `stats` on llm-data-axi, 623 JSON bytes → 221 TOON bytes (**64.5%** saved); on cobra, 735 → 268 (**63.5%**).
- Largest absolute saving: `grep -n "^func " .` on llm-data-axi, 128,755 JSON bytes (~32,188 tokens) → 87,314 TOON bytes (~21,828 tokens), 32.2% saved.
- `tree --depth 3` on llm-data-axi: 20,003 → 9,659 bytes (51.7% saved); on cobra: 11,202 → 7,507 bytes (33.0%).

`tree`, `stats`, `grep` and `multigrep` print absolute paths in both formats, so figures shift with where the repository sits on disk; compare runs made from the same location.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-data-axi/main/install.sh | sh
```

Prebuilt binaries for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64` and `windows/amd64` (best effort, no guarantee), checked against the release's `SHA256SUMS.txt` before install. Build from source with `make install`.

## Links

- Repository: https://github.com/samestrin/llm-data-axi
- Extracted from: https://github.com/samestrin/llm-tools
- Benchmark: `benchmarks/RESULTS.md` in the repository
