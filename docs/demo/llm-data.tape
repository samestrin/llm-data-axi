# VHS tape for the llm-data demo.
#
# Render it from the repository root:
#
#     make build
#     vhs docs/demo/llm-data.tape
#
# It writes docs/demo/llm-data.gif, the file the README's ## Demo section shows.
#
# The hidden setup below puts ./build first on PATH, so every command in the
# recording is the binary `make build` just wrote and no installed copy is used.
# Build it from the release tag to record the released behaviour. The setup then
# enters docs/demo/example, the committed project the session runs against, so
# every render shows the same files. The grep row still prints the absolute path
# of the checkout it is rendered from, so the recording depends on where it is
# rendered; see docs/demo/README.md before rendering from a long path.
#
# tests/demo/demo_test.go replays every Type line below without vhs, so
# `go test ./tests/demo` fails when a command here stops working. That is also
# why no Type line may contain a double quote: the replay reads the text between
# the first and last quote on the line. docs/demo/README.md explains the
# recording, and the release checklist says when to render it again.

Output docs/demo/llm-data.gif

Set Shell "bash"
Set FontFamily "JetBrains Mono"
Set FontSize 18
Set Width 1000
Set Height 620
Set Padding 24
Set MarginFill "#1e1e2e"
Set BorderRadius 8
Set Theme "Catppuccin Mocha"
Set TypingSpeed 30ms
Set PlaybackSpeed 1.5

Hide
Type "root=$(git rev-parse --show-toplevel)" Enter
Type "export PATH=$root/build:$PATH PS1='llm-data demo $ '" Enter
Type "cd $root/docs/demo/example" Enter
Type "clear" Enter
Show

Type "llm-data detect --path ." Enter
Sleep 2s

Type "llm-data grep start_server . --axi -n" Enter
Sleep 2s

Type "llm-data yaml get --file config.yaml server" Enter
Sleep 3s

Hide
Type "rm -f config.yaml.lock" Enter
