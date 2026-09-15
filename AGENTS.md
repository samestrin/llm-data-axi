# Working on llm-data

House rules for an agent making changes **to this repository**.

## What this is

A general-purpose data and project CLI built to the [AXI](https://axi.md) principles. One rule sits above the rest:

**Nothing may report success when it did not succeed.**

A `grep` that returns nothing because the path was wrong, reported the same way as a `grep` that genuinely found nothing. An encode that emits an empty document and exits 0. A flag that is accepted and silently ignored. Each of those is the same bug wearing different clothes. If you are weighing a design choice, that is the tiebreaker.

The sharpest instance this repo has already fixed: the `llm-tools` MCP server appended `--json --min` to every command and no handler ever passed `--axi`, so a client asking for TOON got JSON with no error and exit 0.

## Commands

```bash
make lint        # gofmt -s -l and go vet
make test-race   # race detector — CI runs this
make build       # both binaries into ./build/ — required before the MCP e2e tests run
make test-cover  # coverage profile and total
```

CI runs `gofmt -s -l`, `go vet`, and `go test -race`, then **fails the build below 80% coverage**. That gate is a hard fail, not a warning. It builds into `./build/` first, because the MCP end-to-end tests exec the real binary.

### Measuring coverage honestly

Measure cold and with the LLM environment stripped, or the number lies:

```bash
go clean -testcache && env -u OPENAI_API_KEY -u ANTHROPIC_API_KEY \
  -u OPENAI_BASE_URL -u OPENAI_MODEL go test -coverprofile=/tmp/ci.out ./...
go tool cover -func=/tmp/ci.out | tail -1
```

A cached run writes a partial profile and reads low. An exported API key can push a test down a path CI never takes and read high. In the sibling repo the same suite read 85.0% locally and 80.3% in CI for exactly that reason.

## Testing

Write the failing test first. Every test carries a comment naming what it defends — usually the bug it prevents.

Five things worth knowing before you add one:

1. **Assert side effects, not exit codes.** A command test that only checks for exit 0 passes even when the command printed nothing. Check the payload.
2. **Tests are not parallel-safe.** `internal/data/commands` swaps package globals (`GlobalAXIOutput`, `GlobalJSONOutput`, `GlobalMinOutput`, and each command's own flag vars). Never add `t.Parallel()` there, and restore any global you set with `t.Cleanup`.
3. **No network in tests.** There is no LLM endpoint in CI. `extract-relevant` and `extract-links` are tested against `httptest` fakes. **Never write a test that `t.Skip`s on an environment variable** — that is what makes a local number and a CI number disagree.
4. **Never mix a real path with a `t.TempDir()` path.** On macOS `t.TempDir()` sits behind `/var -> /private/var`, and `os.Getwd` resolves it. Call `filepath.EvalSymlinks` on the temp dir before comparing.
5. **Prove a test can fail.** Break the code deliberately and confirm it goes red. A test that passes against sabotaged code is not defending anything — one assertion here about stripped HTML chrome passed against sabotaged code and had to be rewritten.

## Technical debt

`.planning/technical-debt/README.md` is the record. It is the one thing under `.planning/` that is committed, because the validation pipeline runs in a disposable worktree built from the pushed branch and cannot see an untracked file.

**When you resolve an item, update it in the same change that fixes it.** Mark the row `[x]`, rewrite the entry to say what was done while keeping the original report readable, and correct the counts. A debt file that lags reality is worse than none, because it is trusted.

When you find something you are not fixing, file it: severity, `file:line`, what is wrong, and how to reproduce. Severity is about consequence — **HIGH** means a caller can be misled about what happened.

A partial fix does not earn a `[x]`. An item naming two commands or two call sites stays open until both are done, or it is split so the unfinished half stays visible.

## Scope

Prefer the smallest change that solves the problem. If a fix starts pulling in adjacent work, say so and ask rather than expanding quietly.

This repository was extracted from `llm-tools` and the extraction is not finished across the wider toolchain. Do not port behaviour back and forth between the two; `llm-tools` is frozen on go-axi v0.1.2 and is being archived. In particular, **a change here is not a change there** — the two copies of these commands will diverge and that is expected.

Twenty-six tools travelled. The rest stayed behind on purpose, because no consumer calls them. Adding one back is cheap and reversible; do it when something actually needs it, not pre-emptively.
