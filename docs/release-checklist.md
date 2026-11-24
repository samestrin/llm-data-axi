# Release checklist

Steps for cutting a release of `llm-data`. Run steps 1 to 4 from the repository root on the commit that will be tagged; step 5 publishes the release, and step 6 follows its publication.

## 1. Green before a tag

```bash
make lint
make test-race
make test-cover
```

CI runs the same checks on `main` (`.github/workflows/test.yml`) and fails below 80% coverage; a release is never built from a commit whose tests fail.

## 2. Re-render the demo when a command it shows changes

`docs/demo/llm-data.gif` records `llm-data detect`, `llm-data grep --axi` and `llm-data yaml get`. Re-render it when any of those commands, their output, or the project they run against (`docs/demo/example/`) changed since the GIF was last rendered.

```bash
make build
vhs docs/demo/llm-data.tape
```

- [ ] `vhs` is installed with `ttyd` and `ffmpeg` (see `docs/demo/README.md`).
- [ ] The render finishes and the session is under 30 seconds.
- [ ] Every command in it succeeds; no error text in the recording.
- [ ] The `grep` row fits on one line (it prints paths relative to the example project, so it no longer depends on the checkout path — see `docs/demo/README.md`).
- [ ] `docs/demo/llm-data.gif` is committed with the tape change.
- [ ] No hosted recording link (`vhs.charm.sh`) was added anywhere.

The tape's commands are replayed without `vhs` by `go test ./tests/demo`, so a command that stopped working fails CI before the render is needed.

## 3. CHANGELOG

- [ ] `CHANGELOG.md` has an `[Unreleased]` section that lists every user-visible change since the last tag, in plain words, without ticket IDs.
- [ ] That section is renamed to the version and the release date before step 4, in the commit that will be tagged: the benchmark guards read the newest dated heading as the release.

## 4. Benchmark the release candidate

Measure the commit that will be tagged, before the tag, so the tag carries its own figures: a tag whose results, README and catalog entry still describe the previous release is the stale-docs failure v2.0.1 shipped with. Work in a clean checkout of that commit (a fresh `git worktree add` or clone): the token report refuses a checkout with uncommitted changes, because its commit would not name what was measured.

```bash
make build VERSION=v<version>
build/llm-data --version
go run ./benchmarks -perf -bin build/llm-data
go run ./benchmarks -bin build/llm-data -o benchmarks/RESULTS.md
```

`VERSION=v<version>` stamps the candidate with the release version; an unstamped build reports `dev`, which the latest-release guard rejects.

- [ ] `build/llm-data --version` prints the release version.
- [ ] The regenerated `benchmarks/results-performance.*` files and the README Performance table are committed, and both result files name the release version, not `dev`.
- [ ] `benchmarks/RESULTS.md` is regenerated from a clean checkout and names the binary version and the commit it measured, and the catalog entry's token figures (`docs/axi-catalog-entry.yaml`, description and observation) are re-quoted from its total row.
- [ ] The README pins, the AXI audit's line cites and the catalog entry's observations are re-checked against the candidate.

The commit that adds the results is the one to tag; the measured commit is its parent, and `RESULTS.md` names it, so the table can be reproduced by checking that commit out.

## 5. Tag and publish

```bash
git tag v<version>
git push origin v<version>
```

The `release` workflow tests the tagged commit, builds `llm-data` and `llm-data-mcp` for every platform, writes `SHA256SUMS.txt`, and publishes them as the release. Check the run is green before announcing anything.

- [ ] The release workflow run for the tag is green.
- [ ] The release page lists both binaries per platform plus `SHA256SUMS.txt`.
- [ ] The binaries report the tagged version (`llm-data --version`).

## 6. Check the install path

- [ ] On a machine without a checkout, the install one-liner installs the new version from the new release:

      ```bash
      curl -fsSL https://raw.githubusercontent.com/samestrin/llm-data-axi/main/install.sh | sh
      llm-data --version
      ```

- [ ] A checksum mismatch or a failed download still installs nothing (the installer's own tests cover this, but the published assets are what it reads).
- [ ] After the tag, re-pin `reviewed_revision` in `docs/axi-catalog-entry.yaml` to `git rev-parse v<version>^{commit}`.
