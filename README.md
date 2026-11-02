# llm-data

> Deterministic file, project and config tooling for agents. One token-dense output format.

![Go](https://img.shields.io/github/go-mod/go-version/samestrin/llm-data-axi)
![License](https://img.shields.io/github/license/samestrin/llm-data-axi)

A general-purpose data CLI built to the [AXI](https://axi.md) principles, so its output is cheap for a model to read and hard for a model to misread. It answers the questions an agent asks constantly and should never guess at: does this path exist, what is in this directory, what type of project is this, what changed in git, what does this YAML key say.

Every command is deterministic. None of them calls a model or opens a network connection.

Two binaries:

- `llm-data` — the CLI. This is the product.
- `llm-data-mcp` — an MCP server that shells out to the CLI. A fallback for environments that cannot run a CLI, not the primary surface.

Extracted from [samestrin/llm-tools](https://github.com/samestrin/llm-tools), which carried three unrelated tools in one Go module. Installing the file helper used to drag in sqlite, qdrant and tree-sitter. That is why it now lives here.

## Install

```bash
git clone https://github.com/samestrin/llm-data-axi.git
cd llm-data-axi
sudo ./install.sh
```

Or build only:

```bash
make build     # binaries land in ./build/
```

## Usage

```bash
# does any of this exist
llm-data multiexists src/main.go docs/README.md --axi

# what kind of project is this
llm-data detect --path . --axi

# search
llm-data grep "func New" internal/ -n
llm-data multigrep --keywords "retry,backoff,timeout" --path . --axi

# git
llm-data git-changes --axi
llm-data git-context --max-commits 5 --axi
llm-data repo-root

# config
llm-data yaml get --file config.yaml server.port
llm-data yaml multiset --file config.yaml server.port 8080 server.host localhost
```

### Commands

`catfiles` · `count` · `detect` · `diff-smell` · `discover-tests` · `git-changes` · `git-context` · `grep` · `highest` · `math` · `multiexists` · `multigrep` · `plan-type` · `project-components` · `repo-root` · `runtime` · `stats` · `template` · `tier-classifier` · `tree` · `yaml`

`yaml` carries its own subcommands: `get`, `set`, `multiget`, `multiset`, plus `init`, `list`, `delete`, `validate`, `push` and `pop`.

Run `llm-data <command> --help` for the flags.

## Output modes (AXI)

| Flag | Output |
|---|---|
| `--axi` | TOON — token-dense, the format to give a model |
| `--json` | JSON |
| `--min` | minimal / token-optimized |
| *(none)* | human-readable text |

`--axi` wins when combined with `--json`. TOON encoding goes through [go-axi](https://github.com/samestrin/go-axi), which guards against the failure mode the raw codec has: a type it cannot encode does not error, it emits **empty output**, and a command then silently prints nothing.

`--axi` is a persistent flag on the root, so every command accepts it.

### Exit codes, and one stated exception

A command that ran reports 0; a command that failed reports 1 with a structured error (`error: true` plus `message:`) on stdout in `--json` and `--axi`, and on stderr in plain mode. The exception: a malformed invocation — an unknown flag or command — exits **2** with the error on stderr even under `--axi`. That is the go-axi usage-error convention, kept deliberately.

## A bare run: the home view

`llm-data` with no arguments prints two lines — `bin:` (the executable path, home directory written as `~`) and `description:` (one sentence) — instead of a help page. `--json` and `--axi` add a `help` list of next steps. `--help` still prints the full help, and an unknown subcommand still fails with exit 2.

## Setup

```bash
llm-data setup            # in the project directory
llm-data setup --dry-run  # show what would be written
```

`setup` registers llm-data with Claude Code in the current project: it merges a SessionStart hook (a bare `llm-data`, so the home view lands in the session context) into `.claude/settings.json` — every other key and hook is kept — and writes an Agent Skill at `.claude/skills/llm-data/SKILL.md`. Only files inside the project directory are written; `~/.claude` is never touched, and running it in the home directory itself is refused. It is safe to run again: a second run changes no bytes and says so. It never prompts.

## The MCP server

```bash
llm-data-mcp        # speaks MCP over stdio
```

It resolves `llm-data` from `PATH`, falling back to `/usr/local/bin/llm-data`. Set `LLM_DATA_BINARY` to point it somewhere else.

By default it asks the CLI for TOON (`--axi`). To get `--json --min` instead, either set `LLM_DATA_MCP_AXI=false` for the whole server, or pass `"format": "json"` on a single tool call; `"format": "axi"` asks for TOON on one call when the server is set to JSON. Any other `format` value is an error, except on `runtime`, whose `format` is its time format. A `LLM_DATA_MCP_AXI` value that is not a boolean keeps TOON and logs a warning to stderr. The `llm-tools` server appended `--json --min` unconditionally, which made `--axi` unreachable through MCP no matter what the CLI supported.

Set `LLM_DATA_MCP_DEBUG=true` to log each command line and timeout to stderr.

## Known AXI gaps

The audit in [`docs/axi-audit.md`](docs/axi-audit.md) scored the CLI against the ten AXI principles. These are still **not met**, stated here so no caller learns them the hard way:

- **No truncation.** `grep` emits each matching line whole in every mode, and no command has a `--full` flag. `multigrep` cuts content at 80 characters in plain mode only; `git-context` cuts the diff at 50,000 bytes with no way to page the rest.
- **No totals on `grep`, and silent zeros elsewhere.** `grep` reports no match count in any mode. `multigrep` carries `total_matches` in `--json`/`--axi` but drops it when it is zero (`omitempty`), and a keyword with no matches drops its `match_count` too. A zero-match `grep` prints only the pattern — there is no explicit `0`.
- **`yaml push` is not idempotent.** Pushing the same value twice appends it twice, and both runs report success. `yaml init` and `yaml set` are idempotent.

## Where did `extract-relevant` and `extract-links` go?

They moved to [llm-extract-axi](https://github.com/samestrin/llm-extract-axi), which ships them as `llm-extract extract-relevant` and `llm-extract extract-links` (MCP: `extract_relevant` and `extract_links` on `llm-extract-mcp`). The copies here, and the `llm_data_extract_*` MCP tools, are removed. Everything that remains in this repository is deterministic and never touches the network.

## Development

```bash
make test        # go test ./...
make test-race   # race detector — CI runs this
make lint        # go vet + gofmt check
make test-cover  # coverage profile and total
make hooks       # opt in to the pre-commit hook
```

CI gates coverage at 80% and fails below it. See `AGENTS.md` for house rules.

## License

MIT
