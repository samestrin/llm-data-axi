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
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-data-axi/main/install.sh | sh
```

This downloads the release binaries for your OS and architecture into `~/.local/bin`, checks them against the release's `SHA256SUMS.txt`, and needs no sudo. To pin a release, put the variable after the pipe so it reaches `sh`, not `curl`:

```bash
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-data-axi/main/install.sh | LLM_DATA_VERSION=v1.0.0 sh
```

Set `LLM_DATA_INSTALL_DIR` to install elsewhere. The script itself covers Linux and macOS on amd64 and arm64; on Windows it tells you to grab the `.exe` from the release page.

Or build from source:

```bash
make build     # binaries land in ./build/
make install   # build, then install to /usr/local/bin (may need sudo)
```

**Support is best effort.** Releases ship binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64 and windows/amd64 (the matrix in [`release.yml`](.github/workflows/release.yml)), but there is no support guarantee: a platform that misbehaves may simply stop being built, and anything outside that matrix — another OS, another architecture — means building from source yourself.

## Usage

```bash
# does any of this exist
llm-data multiexists src/main.go docs/README.md

# what kind of project is this
llm-data detect --path .

# search
llm-data grep "func New" internal/ -n
llm-data multigrep --keywords "retry,backoff,timeout" --path .

# git
llm-data git-changes
llm-data git-context --max-commits 5
llm-data repo-root

# config
llm-data yaml get --file config.yaml server.port
llm-data yaml multiset --file config.yaml server.port 8080 server.host localhost
```

### Commands

`catfiles` · `count` · `detect` · `diff-smell` · `discover-tests` · `git-changes` · `git-context` · `grep` · `highest` · `math` · `multiexists` · `multigrep` · `plan-type` · `project-components` · `repo-root` · `runtime` · `stats` · `template` · `tier-classifier` · `tree` · `yaml`

`yaml` carries its own subcommands: `get`, `set`, `multiget` and `multiset`.

Run `llm-data <command> --help` for the flags.

## Output modes (AXI)

| Flag | Output |
|---|---|
| *(none)* | TOON — token-dense, the default |
| `--axi` | TOON (accepted alias; the same output as no flag) |
| `--json` | JSON |
| `--plain` | plain text (the default before TOON was) |
| `--min` | shorter `--json` or `--plain` output; no effect on TOON |

`--plain` cannot be combined with `--json` or `--axi`: the combination is a usage error (exit 2) whose message names both flags. `--axi` wins when combined with `--json`. TOON encoding goes through [go-axi](https://github.com/samestrin/go-axi), which guards against the failure mode the raw codec has: a type it cannot encode does not error, it emits **empty output**, and a command then silently prints nothing.

The format flags are persistent on the root, so every command accepts them.

### Exit codes

A command that ran reports 0; a command that failed reports 1 with a structured error (`error: true` plus `message:`) on stdout — in TOON by default, JSON under `--json`, and text (`Error: ...`) under `--plain`. stderr carries diagnostics only. A partial result is a failure too: `template --strict` with an undefined variable, `catfiles` with a path it cannot read, and `multigrep --output-dir` with a keyword file it cannot write each exit 1, and the error lists the undefined names, the unreadable paths or the failed writes (`undefined`, `unreadable`, `failed_writes` in TOON and `--json`). A malformed invocation — an unknown flag or command, a value a flag cannot take, or conflicting format flags — exits **2**, the usage-error code the AXI spec itself asks for ("unknown flags must fail loud (exit 2)"). It is reported the same way as any other error, on stdout in the active format. An unknown flag's error also lists the flags that command accepts, in the message and as a `valid_flags` list in TOON and `--json`:

```
$ llm-data grep --nope
error: true
message: "unknown flag: --nope; valid flags for llm-data grep: --axi, -l/--files-with-matches, ..."
valid_flags[10]: "--axi","-l/--files-with-matches",...
```

A bad value names the flag, the value and what the flag takes:

```
$ llm-data runtime --start abc
error: true
message: "invalid value \"abc\" for --start: want an integer"
```

## A bare run: the home view

`llm-data` with no arguments prints `bin:` (the executable path, home directory written as `~`) and `description:` (one sentence) instead of a help page, followed by the repository it was run in:

- inside a git repository: `repo_root:` (home written as `~`), `project_type:` (the `detect` stack) and `git_changes:` (the `git-changes` count, `0` for a clean tree);
- outside one: `repo: no repository`, printed only when git itself says so;
- when git is missing, fails or does not answer: `repo: git unavailable: <reason>`, never "no repository". Each of the two git calls has 2 seconds, and a timeout names the call, as in `git status gave no answer within 2s`.

Every case exits 0. TOON (the default) and `--json` carry the same fields plus a `help` list of next steps; `--plain` leaves the list out. `--help` still prints the full help, and an unknown subcommand still fails with exit 2.

## Setup

```bash
llm-data setup            # in the project directory
llm-data setup --dry-run  # show what would be written
llm-data setup --target claude  # name the agents to register with; claude is the default
llm-data setup --target claude,codex  # register with Claude Code and Codex
llm-data setup --target opencode  # register with OpenCode
```

`setup` registers llm-data with Claude Code in the current project: it merges a SessionStart hook (which runs `llm-data`, so the home view lands in the session context) into `.claude/settings.json` — every other key and hook is kept — and writes an Agent Skill at `.claude/skills/llm-data/SKILL.md`. `--target codex` registers it with Codex instead, or as well: the same SessionStart hook merged into `.codex/hooks.json`, and `[features] hooks = true` merged into `.codex/config.toml`, without which Codex does not run that hook. The TOML is edited line by line, so comments and other keys are kept; a config `setup` cannot change that way (invalid TOML, or `features` set as a dotted key or inline table) is refused and nothing is written. `--target opencode` writes a plugin at `.opencode/plugins/llm-data.js` that runs `llm-data` once per session and adds the home view to OpenCode's system context; it adds no tool, and if the home view fails it adds an error line saying so. The plugin is `setup`'s own file and is rewritten whole when it differs; a file at that path that `setup` did not write is refused and nothing is written. The hook (and the plugin) runs the bare `llm-data` when `PATH` finds it, otherwise the absolute path of the binary that ran `setup`; a hook `setup` wrote earlier that no longer matches, such as the old path after the binary moved, is rewritten in place rather than added again. An unknown `--target` is a usage error (exit 2) and writes nothing. Only files inside the project directory are written; `~/.claude`, `~/.codex` and `~/.config/opencode` are never touched, and running it in the home directory itself is refused. It is safe to run again: a second run changes no bytes and says so. It never prompts.

## The MCP server

```bash
llm-data-mcp        # speaks MCP over stdio
```

It looks for `llm-data` in `LLM_DATA_BINARY` (if set), on `PATH`, beside the `llm-data-mcp` executable, and finally at `/usr/local/bin/llm-data`. The sibling step means an install into `~/.local/bin` (the installer's default) works even when that directory is not on `PATH`. If the binary is nowhere, the startup error names the path tried, the lookup order and `LLM_DATA_BINARY`.

By default it asks the CLI for TOON (`--axi`). To get `--json --min` instead, either set `LLM_DATA_MCP_AXI=false` for the whole server, or pass `"format": "json"` on a single tool call; `"format": "axi"` asks for TOON on one call when the server is set to JSON. Any other `format` value is an error, except on `runtime`, whose `format` is its time format. A `LLM_DATA_MCP_AXI` value that is not a boolean keeps TOON and logs a warning to stderr. The `llm-tools` server appended `--json --min` unconditionally, which made `--axi` unreachable through MCP no matter what the CLI supported.

Set `LLM_DATA_MCP_DEBUG=true` to log each command line and timeout to stderr.

## Known AXI gaps

The audit in [`docs/axi-audit.md`](docs/axi-audit.md) scores the CLI against the ten AXI principles. Every principle is met except two stated, justified remainders:

- **No session-end lifecycle-capture hook.** `llm-data` keeps no session state — no transcript, no record of files touched — so there is nothing for a session-end hook to capture. The session-start integration (the home view as context) ships for Claude Code, Codex and OpenCode through `setup`.
- **`help[]` next steps are scoped, not universal.** They sit where a result leaves an agent without a next step: the home view, empty search results and truncated search lines. Widening them to every command is deliberately left as future work.

If a new gap appears, state it here and carry a row for it in the technical-debt record rather than letting a caller learn it the hard way.

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
