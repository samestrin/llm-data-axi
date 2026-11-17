# Demo

`llm-data.gif` in this directory is a real terminal session, recorded from the VHS tape beside it, `llm-data.tape`. The GIF is a file in this repository: there is no hosted recording link to go stale or disappear.

The session shows three commands against the binary `make build` produces, run in `docs/demo/example`:

| Command | What it shows |
| --- | --- |
| `llm-data detect --path .` | the example project's stack and package manager as one TOON document |
| `llm-data grep start_server . --axi -n` | the definition as a `matches[]` table with file, line and text |
| `llm-data yaml get --file config.yaml server` | the `server` mapping from `config.yaml` |

## The example project

`docs/demo/example/` is a small Python project — `requirements.txt`, `app.py` and `config.yaml` — that exists only for this recording. It is committed, so every render runs against the same files, and the tape enters it before the first command.

The files are the same everywhere, but the `grep` row is not: `llm-data grep` prints the absolute path of each match, so that row carries the full path of the checkout the tape is rendered from (`/home/you/src/llm-data-axi/docs/demo/example/app.py,8,...`). The recording therefore depends on where it is rendered. At `Set Width 1000` a short checkout path keeps the row on one line; a long one (such as `/Users/<name>/Documents/GitHub/llm-data-axi`) wraps it and puts the recorder's directory names in the GIF. Render from a short checkout path when that row must stay on one line.

## What the session runs

A hidden step in the tape puts `./build` first on `PATH`, so `llm-data` in the recording is the binary `make build` just wrote, not an installed copy. Build it from the tag of the release being recorded, so the session shows what that release does:

```bash
git checkout <release tag>
make build
```

## Render it

Requires `vhs`, which records through `ttyd` and `ffmpeg`:

```bash
# macOS
brew install vhs ttyd ffmpeg
# Debian/Ubuntu
sudo apt install ttyd ffmpeg && go install github.com/charmbracelet/vhs@latest
```

Then, from the repository root:

```bash
make build
vhs docs/demo/llm-data.tape
```

That writes `docs/demo/llm-data.gif`. Commit it with the tape change. The session is under 30 seconds; if a change pushes it past that, trim a `Sleep`, not a command.

Re-render it when a command the demo shows, its output, or `docs/demo/example` changes. `docs/release-checklist.md` makes that part of each release.

`yaml get` leaves an empty `config.yaml.lock` beside the file it reads — a known side effect of its file lock, recorded in the technical-debt list — so the tape removes that file in a hidden step after the last command. A render leaves the checkout clean.

## Check it without vhs

`vhs` is not installed on the build hosts, and a recording cannot be diffed. `tests/demo/demo_test.go` reads the tape, replays every `Type` line of it in one bash session (hidden setup included) against `./build/llm-data`, and checks each command's output. It also fails when the tape stops writing `docs/demo/llm-data.gif`, when a `docs/demo/` file links a hosted recording, or when the tape's timings add up to 30 seconds or more.

```bash
make build
go test ./tests/demo -v
```

