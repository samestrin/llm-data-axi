---
name: llm-data
description: Deterministic file, search and project-analysis commands. Use to search code (grep, multigrep), count or list files (count, tree, catfiles), read and edit YAML, inspect git changes, or detect a project's stack, when you need exact results instead of guesses.
---

# llm-data

Run `llm-data --help` for the full command list. Add `--axi` for token-dense TOON output, or `--json` for JSON.

- `llm-data grep <pattern> <path>...` and `llm-data multigrep` search files; a missing path is an error, not "no matches".
- `llm-data tree`, `llm-data count` and `llm-data catfiles` list, count and read files.
- `llm-data yaml get|set|multiget|multiset` reads and edits YAML in place.
- `llm-data git-changes`, `llm-data git-context` and `llm-data repo-root` describe the repository.
- `llm-data detect` and `llm-data project-components` describe the project's stack and layout.
