# llm-data

> Deterministic file, project and config tooling for agents. One token-dense output format.

![Go](https://img.shields.io/github/go-mod/go-version/samestrin/llm-data-axi)
![License](https://img.shields.io/github/license/samestrin/llm-data-axi)

A general-purpose data CLI built to the [AXI](https://axi.md) principles, so its output is cheap for a model to read and hard for a model to misread. It answers the questions an agent asks constantly and should never guess at: does this path exist, what is in this directory, what type of project is this, what changed in git, what does this YAML key say.

Every command is deterministic. Two of them — `extract-relevant` and `extract-links --context` — call an LLM API; the other twenty-four do not.

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

`catfiles` · `count` · `detect` · `diff-smell` · `discover-tests` · `extract-links` · `extract-relevant` · `git-changes` · `git-context` · `grep` · `highest` · `math` · `multiexists` · `multigrep` · `plan-type` · `project-components` · `repo-root` · `runtime` · `stats` · `template` · `tier-classifier` · `tree` · `yaml`

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

## The MCP server

```bash
llm-data-mcp        # speaks MCP over stdio
```

It resolves `llm-data` from `PATH`, falling back to `/usr/local/bin/llm-data`. Set `LLM_DATA_BINARY` to point it somewhere else.

By default it asks the CLI for `--json --min`, because that is what registered MCP clients parse. To get TOON instead, either set `LLM_DATA_MCP_AXI=true` for the whole server, or pass `"format": "axi"` on a single tool call. The `llm-tools` server appended `--json --min` unconditionally, which made `--axi` unreachable through MCP no matter what the CLI supported.

Set `LLM_DATA_MCP_DEBUG=true` to log each command line and timeout to stderr.

## LLM configuration

`extract-relevant` and `extract-links --context` call an OpenAI-compatible endpoint. Configuration is **environment only** — there is no config file:

| Variable | Meaning |
|---|---|
| `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` | required |
| `OPENAI_BASE_URL` | default `https://api.openai.com/v1` |
| `OPENAI_MODEL` | default `gpt-4o-mini` |

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
