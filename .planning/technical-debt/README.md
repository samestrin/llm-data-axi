# Technical debt

The record for this repository. Committed on purpose: the validation pipeline runs in a disposable worktree built from the pushed branch, so an untracked file would be invisible to it.

Severity is about consequence. **HIGH** means a caller can be misled about what happened.

## Stats

| | Open | Resolved | Total |
|---|---|---|---|
| HIGH | 1 | 1 | 2 |
| MEDIUM | 5 | 0 | 5 |
| LOW | 4 | 0 | 4 |
| **Total** | **10** | **1** | **11** |

## Open

- [ ] **HIGH** — `internal/data/commands/extract_links.go:382` — `rankLinksWithLLM` sits at 6.7% coverage. It is one of only two places in this repository that makes a network LLM call, it is reached by `extract-links --context`, and essentially none of its body is exercised. `extract-relevant` now has a full `httptest` fake (`extract_relevant_e2e_test.go`); the same treatment applied here would cover the ranking prompt, the parse of the model's reply, and what happens when the model returns something unparseable. Until then the failure modes of a live LLM call on this path are unknown.

- [ ] **MEDIUM** — `internal/data/commands/yaml.go` and `yaml_helpers.go` — both files were taken **whole**, 2,440 lines, to serve four tools (`yaml get`, `yaml set`, `yaml multiget`, `yaml multiset`). They carry six subcommands nobody asked for: `init`, `list`, `delete`, `validate`, `push`, `pop`. That was the right call for the port — a surgical split is 2,440 lines of hand-editing against a 3,410-line test file for no immediate gain — but the surface is now larger than the port rule intended, and `yaml init` writes a template referring to this project's own conventions. Decide whether the six stay as a feature or go.

- [ ] **MEDIUM** — `internal/data/mcpserver/tools.go` and `handlers.go` — the registered tool list and the `buildArgs` dispatch switch are two hand-maintained lists in two files with nothing but a test connecting them. `TestEveryRegisteredToolHasADispatcherCase` catches a tool registered with no case, but there is no guard in the other direction: a `case` for a command that is not registered is dead code that nothing reports. A registry map, as `llm-semantic-axi` uses (`commandRegistry` in its `handlers.go`), makes both directions structural instead of tested.

- [ ] **MEDIUM** — `internal/data/commands` — `RootCmd` is a package-level singleton assembled by ~25 `init()` functions, so the command tree cannot be rebuilt per invocation. `ResetOutputModes` exists purely to paper over the consequence: pflag leaves a bound variable untouched when its flag is absent, so a second in-process run inherits the first one's output mode. `llm-filesystem-axi` and `llm-semantic-axi` both build a fresh tree per invocation and need no such reset. `axi_review_test.go` pins the limitation as it actually behaves and says it should be tightened, not deleted, if anyone makes the singleton stateless.

- [ ] **MEDIUM** — `internal/data/utils` — the whole package travelled (7 exported functions) but only `FormatSize` and `IsExcludedDir` are called. `IsValidFilePath`, `IsValidDirPath`, `ExpandPath`, `NormalizePath` and `ParseSize` have no caller in this repository. They are tested, so they cost nothing at the coverage gate, but an untouched exported function reads as API rather than as leftovers. Either delete them or note in the package doc that they are carried deliberately.

- [ ] **MEDIUM** — `internal/data/commands/git_context.go:256` — `getGitRemotes` is at 13.6% coverage. It parses `git remote -v` output, which has a format that varies with the remote type (ssh, https, insteadOf rewrites) and with how many remotes exist. A table test over captured `git remote -v` strings would cost little and covers a parser that currently has almost no pinned behaviour.

- [ ] **LOW** — `internal/data/commands/extract_relevant.go:323` — `processDirectory` computes its context deadline as `timeout * len(files)` seconds. With the default 60-second timeout, a directory of 500 files asks for an 8-hour deadline, and the per-file timeout that the flag names is never actually enforced per file. The batch is concurrency-limited, so the real bound is roughly `timeout * ceil(files/concurrency)`. Not wrong enough to bite yet, but the flag does not mean what its help text says.

- [ ] **LOW** — `internal/data/commands/extract_relevant.go:31-48` — `ExtractRelevantResult` declares every field twice, once long and once abbreviated (`Path`/`P`, `Context`/`Ctx`, `ExtractedParts`/`EP`, and so on), all with `omitempty`. Only the long form is ever populated, so the short fields are dead weight in the type and in every encoding. Minimal-mode abbreviation is already handled generically by `pkg/output`'s `KeyAbbreviations` table, which is where it belongs.

- [ ] **LOW** — `internal/data/commands/git_changes.go:187-200` (AXI branch) and `:211-217` (JSON branch) — both output paths set a nil `Files` to `[]string{}`, and both branches are **unreachable**. `parseGitStatus` is the only constructor of a `GitChangesResult` and always initialises `Files` to `[]string{}`, so nothing nil ever reaches either. Deleting both leaves every test green, which is how this was found — it was the second sabotage while verifying the `omitempty` fix, and the first one that stayed green.

  Not a correctness bug; dead logic that reads as if it were load-bearing, which is worse than dead logic that looks dead. The comments on the struct, the test row and the e2e test have been corrected to say the guarantee comes from `parseGitStatus` plus the struct tag. Decide whether to delete the two branches or keep them as a guard against a future second constructor — this row exists so the choice is made rather than defaulted.

- [ ] **LOW** — `pkg/llmapi/client_test.go` and `streaming_test.go` — the `pkg/llmapi` suite takes ~22 seconds, dominating a 30-second total. The retry defaults are `MaxRetries: 3`, `RetryDelay: 2s`, `RetryBackoff: 2.0`, so a single test of a retryable status code sleeps 2+4+8 seconds of real time. Injecting the delay, or setting `RetryDelay` to a millisecond in tests, would return most of that. Every new test added here already sets `MaxRetries = 0` for this reason.

## Resolved

- [x] **HIGH** — `internal/data/commands/git_changes.go` — **RESOLVED.** `GitChangesResult.Files` carried `json:"files,omitempty"` while `outputGitChanges` set a nil `Files` to `[]string{}` under the comment "Ensure files is never null". Those contradicted: `omitempty` deletes the key outright for an empty slice, so on a clean working tree the payload was `{"count":0}` with no `files` key at all. A consumer iterating `files` hit a key the schema never announced — the exact case the normalisation was written to prevent. The `--axi` path was affected identically, because `output.EncodeAXI` projects through the JSON form and reads the same tag.

  Filed rather than fixed at port time, because `git_changes_test.go` pinned the omission as the contract and the code and its test disagreed about intent. **Sam chose the `files: []` reading.** The `omitempty` was dropped and both rows of `TestGitChangesResultStruct` were updated in the same change, plus `TestGitChangesAXIEmitsAnEmptyListNotNull` added so the fix is pinned on both encodings rather than one.

  **Sabotage found a second, separate piece of dead code while verifying this.** Restoring `omitempty` goes red on four assertions, as it should. But deleting the nil-to-empty normalisation in `outputGitChanges` leaves every test green: `parseGitStatus` is the only constructor of a `GitChangesResult` and always sets `Files` to `[]string{}`, so that branch is unreachable regardless of the tag. It is kept as a cheap guard against a future second constructor, and the comments on the struct, the test row and the e2e test were all corrected to say so — each of them had claimed the normalisation was load-bearing, which was never true.

  Also fixed in the e2e assertion: it originally checked `len(got.Files) != 0` on the decoded struct, which `encoding/json` produces identically for an absent key, an explicit null and an empty list. That assertion could not tell the bug from the fix. It now checks the raw payload for `"files": []`.
