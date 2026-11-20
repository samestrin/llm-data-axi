# llm-data-axi

llm-data is a single Go binary that gives an AI agent, or you, commands to inspect and transform files, projects and configuration, with [AXI](https://axi.md)-compliant output built for agents to parse.

<a href="https://github.com/samestrin/llm-data-axi/actions/workflows/test.yml"><img src="https://github.com/samestrin/llm-data-axi/actions/workflows/test.yml/badge.svg" alt="CI"></a> <a href="https://github.com/samestrin/llm-data-axi/releases/latest"><img src="https://img.shields.io/github/v/release/samestrin/llm-data-axi" alt="Latest release"></a> <a href="LICENSE"><img src="https://img.shields.io/github/license/samestrin/llm-data-axi" alt="License"></a>

## Demo

<img src="docs/demo/llm-data.gif" alt="A terminal session running detect, grep and yaml get">

The under-30-second session runs against the release binary. The [VHS source and render instructions](docs/demo/README.md) are committed with the example project.

## Why

- **Inspect a project before acting.** Detect its stack, tests and Git state with deterministic commands.
- **Search without assembling shell pipelines.** Find one pattern or many keywords, read files and summarize directory contents through one interface.
- **Read and change structured data safely.** Address nested YAML keys, preview writes and substitute template variables without a model call.
- **Give an agent output it can parse.** Choose human-readable text, JSON or token-dense TOON, with consistent exit codes and no network calls.

## Install

### Release binaries

```bash
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-data-axi/main/install.sh | sh
```

The installer places `llm-data` and `llm-data-mcp` in `~/.local/bin` without sudo. Add that directory to `PATH` if the installer reports that it is missing.

The release asset names are `llm-data-<os>-<arch>` and `llm-data-mcp-<os>-<arch>`, with `.exe` appended on Windows. Releases cover `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64` and `windows/amd64` on a best-effort basis.

For each selected platform, the installer downloads `SHA256SUMS.txt`, downloads both matching assets and verifies each SHA-256 checksum before installing either binary. A missing entry or mismatched checksum stops the installation.

Pin the latest release, `v2.0.0`, or choose another installed location:

```bash
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-data-axi/main/install.sh | LLM_DATA_VERSION=v2.0.0 sh
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-data-axi/main/install.sh | LLM_DATA_INSTALL_DIR="$HOME/bin" sh
```

### Go install

Go 1.24.0 or newer is required. These commands install into `GOBIN`, or into `GOPATH/bin` when `GOBIN` is unset:

```bash
go install github.com/samestrin/llm-data-axi/v2/cmd/llm-data@latest
go install github.com/samestrin/llm-data-axi/v2/cmd/llm-data-mcp@latest
```

### From source

```bash
git clone https://github.com/samestrin/llm-data-axi.git
cd llm-data-axi
git checkout v2.0.0
make build
make install
```

`make build` writes both binaries to `./build/`. `make install` copies them to `/usr/local/bin` and may require sudo.

### Agent integration

Run `setup` in a project to register llm-data with each agent named by `--target`, a comma-separated or repeated list of `claude`, `codex` and `opencode`. The default is `claude` alone:

| Target | What `setup` writes |
|---|---|
| `claude` | A SessionStart hook merged into `.claude/settings.json`, and the Agent Skill at `.claude/skills/llm-data/SKILL.md` |
| `codex` | A SessionStart hook merged into `.codex/hooks.json`, and `[features] hooks = true` in `.codex/config.toml` |
| `opencode` | A plugin at `.opencode/plugins/llm-data.js` that adds the home view to OpenCode's system context |

It preserves existing settings, writes only inside the project and leaves bytes unchanged on a second run. An unknown target is a usage error and nothing is written. The project path defaults to `.`:

```bash
llm-data setup
llm-data setup --dry-run
llm-data setup --target claude,codex,opencode
```

Install the same Agent Skill without the hook:

```bash
npx skills add samestrin/llm-data-axi --skill llm-data
```

## Quick start

Run these commands in a Git project directory:

```bash
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-data-axi/main/install.sh | LLM_DATA_VERSION=v2.0.0 sh
export PATH="$HOME/.local/bin:$PATH"
llm-data detect --path . --axi
llm-data git-changes --axi
```

In this repository, the `detect` command prints:

```text
framework: ""
has_tests: true
language: go
package_manager: go
pytest_available: false
stack: go
```

## Usage

| Command | What it does |
|---|---|
| `catfiles` | Concatenate multiple files with headers |
| `count` | Count checkboxes, lines, or files |
| `detect` | Detect project type and technology stack |
| `discover-tests` | Discover test patterns and infrastructure |
| `git-changes` | Count and list git working tree changes |
| `git-context` | Gather git repository context information |
| `grep` | Search for pattern in files |
| `highest` | Find highest numbered directory or file |
| `math` | Evaluate mathematical expressions safely |
| `multiexists` | Check if multiple files/directories exist |
| `multigrep` | Search for multiple keywords in parallel |
| `repo-root` | Find and output git repository root |
| `runtime` | Calculate and format elapsed time from epoch timestamps |
| `setup` | Register llm-data with Claude Code, Codex or OpenCode in this project |
| `stats` | Display directory statistics |
| `template` | Variable substitution in templates |
| `tree` | Display directory tree structure |
| `yaml` | Manage YAML configuration files |

Run `llm-data <command> --help` for every flag and default. These examples show the command shapes:

```bash
llm-data catfiles README.md docs/axi-audit.md --max-size 10 --axi
llm-data count --path . --mode files --recursive --axi
llm-data detect --path . --axi
llm-data discover-tests --path . --axi
llm-data git-changes --include-untracked --axi
llm-data git-context --max-commits 5 --include-diff --axi
llm-data grep "func New" internal/ -n --axi
llm-data highest --path .planning/plans/active --type dir --axi
llm-data math "sqrt(16) + 2" --axi
llm-data multiexists README.md docs --verbose --no-fail --axi
llm-data multigrep --keywords "retry,backoff,timeout" --path . --axi
llm-data repo-root --path . --validate --axi
llm-data runtime --start 1760000000 --end 1760000065 --format human --axi
llm-data setup --path . --dry-run --axi
llm-data stats --path . --axi
llm-data template template.txt --var name=Ada --strict --axi
llm-data tree --path . --depth 3 --sizes --axi
```

`yaml` has four subcommands:

```bash
llm-data yaml get --file config.yaml server.port --axi
llm-data yaml set --file config.yaml server.port 8080 --dry-run --axi
llm-data yaml multiget --file config.yaml server.host server.port --axi
llm-data yaml multiset --file config.yaml server.port 8080 server.host localhost --dry-run --axi
```

## Configuration

| Name | Default | Meaning |
|---|---|---|
| `LLM_DATA_VERSION` | latest release | Release tag fetched by `install.sh`, such as `v2.0.0` |
| `LLM_DATA_INSTALL_DIR` | `$HOME/.local/bin` | Directory where `install.sh` places both binaries |
| `LLM_DATA_BASE_URL` | release selected by `LLM_DATA_VERSION` | Alternative release-asset base URL; takes precedence over `LLM_DATA_VERSION` |
| `LLM_DATA_BINARY` | automatic lookup | Explicit `llm-data` path for `llm-data-mcp`; otherwise lookup tries `PATH`, the server binary's directory and `/usr/local/bin/llm-data` |
| `LLM_DATA_MCP_AXI` | `true` | Requests TOON from the CLI; `false` selects `--json --min` |
| `LLM_DATA_MCP_DEBUG` | disabled | Set to `true` to log each MCP command line and timeout to stderr |

No API key or other secret is used.

## Output and exit codes

| Flag | Output |
|---|---|
| none | Token-dense TOON, the default |
| `--axi` | TOON, the same as no flag (accepted alias) |
| `--json` | JSON |
| `--plain` | Human-readable text; cannot be combined with `--json` or `--axi` (usage error, exit 2) |
| `--min` | Shorter `--json` or `--plain` output; no effect on TOON |
| `--absolute` | Absolute paths in `tree`, `stats`, `multigrep`, `grep`, `catfiles`, `highest` and `git-context`; by default they print paths relative to the working directory, and a path outside it stays absolute. Other commands reject the flag; `repo-root` always prints an absolute root |

A bare `llm-data` prints the executable path, a description and the current repository context instead of a help page. Use `llm-data --help` for command help.

| Exit code | Meaning |
|---:|---|
| 0 | The command completed, including a valid empty result |
| 1 | The command failed; the error identifies the failure |
| 2 | Usage error, such as an unknown flag, invalid value or missing required argument |

Errors are written to stdout in the selected output format: TOON by default, JSON with `--json`, and `Error: ...` with `--plain`. A caller reads one stream whatever the format; stderr carries diagnostics only.

`llm-data-mcp` exposes the same commands over MCP stdio. Start it with `llm-data-mcp`; it invokes the CLI rather than reimplementing command behavior. A tool call can request `"format": "axi"` or `"format": "json"` to override the server default for that call, except on `runtime`, where `format` selects the elapsed-time format. A command that exits non-zero comes back as an error result (`isError: true`) carrying the CLI's own error output.

## Performance

These are the committed measurements for `llm-data 1.1.0`, copied from the [generated performance table](benchmarks/results-performance.md). The raw timings are in [results-performance.json](benchmarks/results-performance.json), and [benchmarks/main.go](benchmarks/main.go) contains the measurement and report generator.

- **Date:** 2026-11-17
- **Machine:** Intel(R) Xeon(R) W-2295 CPU @ 3.00GHz, 36 threads, 141.2 GiB RAM, Debian GNU/Linux 13 (trixie), kernel 7.0.0-38-generic
- **Method:** 3 warmup runs, then 20 timed runs per case. Each timing is the wall-clock time of one `llm-data` process, from start to exit; runs are sequential.

| Case | Command | Input | Runs | Min (ms) | Median (ms) | Mean (ms) | Max (ms) |
|---|---|---|---:|---:|---:|---:|---:|
| cold start | `llm-data --version` | none | 20 | 1.77 | 1.83 | 1.85 | 2.13 |
| typical command | `llm-data detect --path .` | this repository | 20 | 1.84 | 1.94 | 1.95 | 2.19 |
| large input | `llm-data stats --path .` | 3,000 generated files | 20 | 15.46 | 15.83 | 15.97 | 19.45 |

Regenerate both result files with `go run ./benchmarks -perf`, or regenerate only the table from the committed raw timings with `go run ./benchmarks -perf -from-json`. These measurements do not compare another tool. The separate [token benchmark](benchmarks/RESULTS.md) compares TOON and JSON output sizes.

## How it works

The release contains two binaries. `llm-data` runs deterministic file, project, data and Git operations without calling a model or opening a network connection. `llm-data-mcp` is a stdio adapter that locates and invokes that CLI for environments that need MCP.

Most commands are read-only. The writers are `yaml set` and `yaml multiset` for the target YAML file, `setup` for project-local agent files, `template --output` for a rendered file and `multigrep --output-dir` for keyword result files.

The repository was extracted from [llm-tools](https://github.com/samestrin/llm-tools) so installing these data commands does not pull in unrelated storage and parsing dependencies.

## Limits

- There is no session-end lifecycle-capture hook. `llm-data` keeps no transcript, session state or record of files touched, so it has no session history to capture.
- `help[]` next steps are scoped rather than universal. They appear in the home view and selected search results; other successful results do not carry them.
- The release platform matrix is best effort. Another OS or architecture requires a source build.
- Search and directory commands respect `.gitignore` and apply their documented size, entry or line limits unless a flag changes that behavior. Large generated or binary trees may need narrower paths or explicit filter flags.
- `setup` does not remove agent configuration during uninstall. Remove its project-local hook and skill separately when they are no longer wanted.
- `extract-relevant` and `extract-links` live in [llm-extract-axi](https://github.com/samestrin/llm-extract-axi), not in this binary. The commands here remain deterministic and do not call an LLM.
- `plan-type`, `project-components`, `tier-classifier` and `diff-smell` moved to cadence in v2.0.0. Use `cadence plan type`, `cadence plan components`, `cadence tier classify` and `cadence diff smell` instead; calling the old names here fails with an error naming the replacement.

The [AXI audit](docs/axi-audit.md) records the conformance evidence and remaining gaps.

## Documentation

- [AXI audit](docs/axi-audit.md)
- [AXI catalog entry](docs/axi-catalog-entry.yaml)
- [Demo source and rendering](docs/demo/README.md)
- [Release checklist](docs/release-checklist.md)
- [Agent Skill](skills/llm-data/SKILL.md)
- [Token benchmark](benchmarks/RESULTS.md)

## Development

```bash
make build
make test
make test-race
make lint
make test-cover
make hooks
```

CI runs formatting, `go vet` and the race-enabled tests, then fails below 80% total coverage. Repository contribution rules are in [AGENTS.md](AGENTS.md).

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for released and unreleased changes.

## License

MIT; see [LICENSE](LICENSE).
