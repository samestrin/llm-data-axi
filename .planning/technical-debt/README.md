# Technical debt

The record for this repository. Committed on purpose: the validation pipeline runs in a disposable worktree built from the pushed branch, so an untracked file would be invisible to it.

Severity is about consequence. **HIGH** means a caller can be misled about what happened.

## Stats

| | Open | Resolved | Total |
|---|---|---|---|
| HIGH | 2 | 0 | 2 |
| MEDIUM | 5 | 0 | 5 |
| LOW | 3 | 0 | 3 |
| **Total** | **10** | **0** | **10** |

## Open

- [ ] **HIGH** — `internal/data/commands/git_changes.go:25` and `:187-200` — `GitChangesResult.Files` carries `json:"files,omitempty"`, and `outputGitChanges` sets a nil `Files` to `[]string{}` under the comment "Ensure files is never null". Those two are in direct contradiction: `omitempty` deletes the key outright for an empty slice, so on a clean working tree the payload is `{"count":0}` with no `files` key at all, and the normalisation above it is unreachable. Probed: `json.Marshal(R{Count:0, Files: []string{}})` with that tag emits `{"count":0}`.

  The consequence is the one the normalisation was written to prevent — a consumer iterating `files` hits a key that is simply absent, which the schema never announced. The `--axi` path is affected identically, because `output.EncodeAXI` projects through the JSON form first, so the same tag governs both.

  **Not fixed here deliberately.** `git_changes_test.go:371` (`TestGitChangesResultStruct/empty_result_omits_files`) pins the omission as the contract, so the two disagree about intent and a port is the wrong place to pick a winner. Fix: decide which is right, then change the tag and that test together, or delete the dead normalisation. `TestGitChangesEmitsAnEmptyListNotNull` pins the current behaviour and carries this note.

- [ ] **HIGH** — `internal/data/commands/extract_links.go:382` — `rankLinksWithLLM` sits at 6.7% coverage. It is one of only two places in this repository that makes a network LLM call, it is reached by `extract-links --context`, and essentially none of its body is exercised. `extract-relevant` now has a full `httptest` fake (`extract_relevant_e2e_test.go`); the same treatment applied here would cover the ranking prompt, the parse of the model's reply, and what happens when the model returns something unparseable. Until then the failure modes of a live LLM call on this path are unknown.

- [ ] **MEDIUM** — `internal/data/commands/yaml.go` and `yaml_helpers.go` — both files were taken **whole**, 2,440 lines, to serve four tools (`yaml get`, `yaml set`, `yaml multiget`, `yaml multiset`). They carry six subcommands nobody asked for: `init`, `list`, `delete`, `validate`, `push`, `pop`. That was the right call for the port — a surgical split is 2,440 lines of hand-editing against a 3,410-line test file for no immediate gain — but the surface is now larger than the port rule intended, and `yaml init` writes a template referring to this project's own conventions. Decide whether the six stay as a feature or go.

- [ ] **MEDIUM** — `internal/data/mcpserver/tools.go` and `handlers.go` — the registered tool list and the `buildArgs` dispatch switch are two hand-maintained lists in two files with nothing but a test connecting them. `TestEveryRegisteredToolHasADispatcherCase` catches a tool registered with no case, but there is no guard in the other direction: a `case` for a command that is not registered is dead code that nothing reports. A registry map, as `llm-semantic-axi` uses (`commandRegistry` in its `handlers.go`), makes both directions structural instead of tested.

- [ ] **MEDIUM** — `internal/data/commands` — `RootCmd` is a package-level singleton assembled by ~25 `init()` functions, so the command tree cannot be rebuilt per invocation. `ResetOutputModes` exists purely to paper over the consequence: pflag leaves a bound variable untouched when its flag is absent, so a second in-process run inherits the first one's output mode. `llm-filesystem-axi` and `llm-semantic-axi` both build a fresh tree per invocation and need no such reset. `axi_review_test.go` pins the limitation as it actually behaves and says it should be tightened, not deleted, if anyone makes the singleton stateless.

- [ ] **MEDIUM** — `internal/data/utils` — the whole package travelled (7 exported functions) but only `FormatSize` and `IsExcludedDir` are called. `IsValidFilePath`, `IsValidDirPath`, `ExpandPath`, `NormalizePath` and `ParseSize` have no caller in this repository. They are tested, so they cost nothing at the coverage gate, but an untouched exported function reads as API rather than as leftovers. Either delete them or note in the package doc that they are carried deliberately.

- [ ] **MEDIUM** — `internal/data/commands/git_context.go:256` — `getGitRemotes` is at 13.6% coverage. It parses `git remote -v` output, which has a format that varies with the remote type (ssh, https, insteadOf rewrites) and with how many remotes exist. A table test over captured `git remote -v` strings would cost little and covers a parser that currently has almost no pinned behaviour.

- [ ] **LOW** — `internal/data/commands/extract_relevant.go:323` — `processDirectory` computes its context deadline as `timeout * len(files)` seconds. With the default 60-second timeout, a directory of 500 files asks for an 8-hour deadline, and the per-file timeout that the flag names is never actually enforced per file. The batch is concurrency-limited, so the real bound is roughly `timeout * ceil(files/concurrency)`. Not wrong enough to bite yet, but the flag does not mean what its help text says.

- [ ] **LOW** — `internal/data/commands/extract_relevant.go:31-48` — `ExtractRelevantResult` declares every field twice, once long and once abbreviated (`Path`/`P`, `Context`/`Ctx`, `ExtractedParts`/`EP`, and so on), all with `omitempty`. Only the long form is ever populated, so the short fields are dead weight in the type and in every encoding. Minimal-mode abbreviation is already handled generically by `pkg/output`'s `KeyAbbreviations` table, which is where it belongs.

- [ ] **LOW** — `pkg/llmapi/client_test.go` and `streaming_test.go` — the `pkg/llmapi` suite takes ~22 seconds, dominating a 30-second total. The retry defaults are `MaxRetries: 3`, `RetryDelay: 2s`, `RetryBackoff: 2.0`, so a single test of a retryable status code sleeps 2+4+8 seconds of real time. Injecting the delay, or setting `RetryDelay` to a millisecond in tests, would return most of that. Every new test added here already sets `MaxRetries = 0` for this reason.

## Resolved

*(none yet)*
