---
name: llm-data
description: Deterministic file, search and project-analysis commands. Use to search code (grep, multigrep), count or list files (count, tree, catfiles), read and edit YAML, read one section, the frontmatter or the tasks of a markdown file or one value of a JSON file, inspect git changes, or detect a project's stack, when you need exact results instead of guesses.
---

# llm-data

Run `llm-data --help` for the full command list. Output is token-dense TOON by default; add `--plain` for text or `--json` for JSON.

- `llm-data grep <pattern> <path>...` and `llm-data multigrep` search files; a missing path is an error, not "no matches".
- `llm-data tree`, `llm-data count` and `llm-data catfiles` list, count and read files.
- `llm-data yaml get|set|multiget|multiset` reads and edits YAML in place.
- `llm-data markdown headers|section|frontmatter|tasks <file>` reads one part of a markdown file (its headers, one section, its frontmatter or its task list) instead of all of it; `llm-data json query <file> <path>` reads one value from a JSON file.
- `llm-data git-changes`, `llm-data git-context` and `llm-data repo-root` describe the repository.
- `llm-data detect` and `llm-data discover-tests` describe the project's stack, layout and test setup.
- `llm-data stats`, `llm-data multiexists` and `llm-data highest` summarize a directory, check paths exist and find the highest-numbered entry.
- `llm-data math`, `llm-data runtime` and `llm-data template` evaluate arithmetic, format elapsed time and fill in template variables.

Not installed? `go install github.com/samestrin/llm-data-axi/v2/cmd/llm-data@v2.0.2`, or run one command without installing: `go run github.com/samestrin/llm-data-axi/v2/cmd/llm-data@v2.0.2 <command>`.

A failure prints `error: true` and a `message`, often with a `help` list naming the next step (the same keys under `--json`). On exit 1 the command ran and failed; on exit 2 the call itself was malformed (unknown flag, bad value, missing argument), so retrying it unchanged will not help. A result cut short says so with `truncated` and a `help` line; `grep` and `multigrep` return whole lines with `--full`.
