#!/bin/sh
# Drives install.sh against a locally built release directory and checks every
# promise the installer makes. CI runs this as a non-root user inside a clean
# container (.github/workflows/install.yml); it runs the same way on a laptop.
#
#   tests/install/run.sh <release-dir>
#
# <release-dir> is what `make dist` produces: the per-platform binaries plus
# SHA256SUMS.txt. Each case copies it, breaks the copy in one way, and points
# the installer at it through LLM_DATA_BASE_URL. Every case asserts what landed
# (or did not land) in the install directory, not only the exit code: an
# installer that exits 1 after copying a tampered binary into place is the bug
# this harness exists to catch.
set -u

REPO_ROOT=$(cd "$(dirname "$0")/../.." && pwd)
INSTALLER="$REPO_ROOT/install.sh"

if [ $# -ne 1 ] || [ ! -f "$1/SHA256SUMS.txt" ]; then
    echo "usage: $0 <release-dir containing SHA256SUMS.txt>" >&2
    exit 2
fi
RELEASE=$(cd "$1" && pwd)

case "$(uname -s)" in
    Linux) OS=linux ;;
    Darwin) OS=darwin ;;
    *) echo "run.sh: unsupported host $(uname -s)" >&2; exit 2 ;;
esac
case "$(uname -m)" in
    x86_64 | amd64) ARCH=amd64 ;;
    aarch64 | arm64) ARCH=arm64 ;;
    *) echo "run.sh: unsupported host $(uname -m)" >&2; exit 2 ;;
esac
CLI_ASSET="llm-data-$OS-$ARCH"
MCP_ASSET="llm-data-mcp-$OS-$ARCH"

sha256() {
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1"; else shasum -a 256 "$1"; fi | cut -d' ' -f1
}

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

FAILED=0
PASSED=0
fail() { echo "  FAIL: $*"; FAILED=$((FAILED + 1)); }
pass() { echo "  ok: $*"; PASSED=$((PASSED + 1)); }

# A fresh HOME per case, so nothing one case installs can satisfy the next.
new_case() {
    CASE="$WORK/$1"
    mkdir -p "$CASE/home" "$CASE/shim"
    echo "case: $1"
}

# copy_release <name>: a private, writable copy of the release to break.
copy_release() {
    cp -R "$RELEASE" "$CASE/$1"
    echo "$CASE/$1"
}

# run_installer <release-dir> [extra env...]: runs install.sh with the case's
# HOME and shim directory first on PATH. Exit code in RC, output in OUT.
run_installer() {
    dir=$1
    shift
    OUT=$(env HOME="$CASE/home" PATH="$CASE/shim:$PATH" \
        LLM_DATA_BASE_URL="file://$dir" "$@" sh "$INSTALLER" 2>&1)
    RC=$?
}

# installed_files: everything in the install dir, temp files included. An
# interrupted or rejected run must leave this empty.
installed_files() {
    ls -A "$CASE/home/.local/bin" 2>/dev/null
}

expect_nothing_installed() {
    left=$(installed_files)
    if [ -z "$left" ]; then
        pass "$1: install dir is empty"
    else
        fail "$1: install dir holds: $(echo "$left" | tr '\n' ' ')"
    fi
}

expect_failure() {
    if [ "$RC" -ne 0 ]; then
        pass "$1: exit $RC"
    else
        fail "$1: exited 0"
        echo "$OUT" | sed 's/^/    | /'
    fi
}

# --- 1. happy path: both binaries land and --version runs --------------------
# Defends the core promise: non-root, no clone, no sudo, into ~/.local/bin.
new_case happy
cd "$CASE" || exit 1 # not the repo: the installer must not need a clone
run_installer "$RELEASE"
if [ "$RC" -eq 0 ]; then pass "happy: exit 0"; else
    fail "happy: exit $RC"
    echo "$OUT" | sed 's/^/    | /'
fi
for bin in llm-data llm-data-mcp; do
    if [ -x "$CASE/home/.local/bin/$bin" ]; then pass "happy: $bin is executable"; else fail "happy: $bin missing"; fi
done
if ver=$("$CASE/home/.local/bin/llm-data" --version 2>&1) && [ -n "$ver" ]; then
    pass "happy: llm-data --version -> $ver"
else
    fail "happy: llm-data --version failed: $ver"
fi
want=$(sha256 "$RELEASE/$CLI_ASSET")
got=$(sha256 "$CASE/home/.local/bin/llm-data" 2>/dev/null)
if [ "$want" = "$got" ]; then pass "happy: installed bytes match the release"; else fail "happy: installed llm-data differs from $CLI_ASSET"; fi
left=$(installed_files | grep -v -x -e llm-data -e llm-data-mcp)
if [ -z "$left" ]; then pass "happy: no temp files left"; else fail "happy: leftovers: $left"; fi
cd "$WORK" || exit 1

# --- 2. a re-run replaces an existing install --------------------------------
# Defends upgrades: mv over an existing binary must work, not just a first run.
run_installer "$RELEASE"
if [ "$RC" -eq 0 ]; then pass "rerun: exit 0"; else fail "rerun: exit $RC"; fi

# --- 3/4. a tampered binary is refused and nothing is installed --------------
# Defends checksum verification. The MCP case matters as much as the CLI one:
# an installer that verifies and installs one file at a time would put a good
# llm-data in place before it ever looked at the tampered llm-data-mcp.
for asset in "$CLI_ASSET" "$MCP_ASSET"; do
    new_case "tampered-$asset"
    dir=$(copy_release rel)
    printf 'x' >>"$dir/$asset"
    run_installer "$dir"
    expect_failure "tampered $asset"
    case "$OUT" in *"$asset"*) pass "tampered $asset: message names the file" ;; *) fail "tampered $asset: message does not name $asset: $OUT" ;; esac
    expect_nothing_installed "tampered $asset"
done

# --- 5. an asset with no SHA256SUMS.txt entry is refused ---------------------
# Defends against "no entry" being read as "nothing to check".
new_case missing-sum
dir=$(copy_release rel)
grep -v " \*\{0,1\}$MCP_ASSET\$" "$RELEASE/SHA256SUMS.txt" >"$dir/SHA256SUMS.txt"
run_installer "$dir"
expect_failure "missing checksum entry"
expect_nothing_installed "missing checksum entry"

# --- 6. a missing asset fails the download and installs nothing --------------
# Defends curl --fail: a 404 page must never be saved as a binary.
new_case missing-asset
dir=$(copy_release rel)
rm "$dir/$MCP_ASSET"
run_installer "$dir"
expect_failure "missing asset"
expect_nothing_installed "missing asset"

# --- 7. every download uses curl --fail with connect and total timeouts ------
# A logging shim in front of the real curl records each invocation.
new_case curl-flags
: >"$CASE/curl.log"
REAL_CURL=$(command -v curl)
cat >"$CASE/shim/curl" <<EOF
#!/bin/sh
echo "\$*" >>"$CASE/curl.log"
exec "$REAL_CURL" "\$@"
EOF
chmod +x "$CASE/shim/curl"
run_installer "$RELEASE"
if [ "$RC" -eq 0 ]; then pass "curl flags: exit 0"; else fail "curl flags: exit $RC"; fi
calls=$(wc -l <"$CASE/curl.log" 2>/dev/null || echo 0)
if [ "$calls" -ge 3 ]; then pass "curl flags: $calls downloads logged"; else fail "curl flags: expected 3 downloads, logged $calls"; fi
for flag in --fail --connect-timeout --max-time; do
    missing=$(grep -v -c -e "$flag" "$CASE/curl.log")
    if [ "$missing" -eq 0 ]; then pass "curl flags: every call has $flag"; else fail "curl flags: $missing call(s) without $flag"; fi
done

# --- 8. an interrupted run leaves no partial binary --------------------------
# The shim writes half the file to curl's -o target, then kills the installer
# with SIGTERM, as a Ctrl-C or a dropped SSH session would. An existing install
# must survive untouched and no partial file may be left beside it.
new_case interrupted
mkdir -p "$CASE/home/.local/bin"
printf 'old' >"$CASE/home/.local/bin/llm-data-mcp"
cat >"$CASE/shim/curl" <<'EOF'
#!/bin/sh
out=""; url=""
while [ $# -gt 0 ]; do
    case "$1" in
        -o) out=$2; shift ;;
        -*) ;;
        *) url=$1 ;;
    esac
    shift
done
src=${url#file://}
case "$url" in
    *llm-data-mcp-*)
        head -c 1000 "$src" >"$out"
        kill -TERM "$PPID"
        sleep 5
        exit 1 ;;
    *) cat "$src" >"$out" ;;
esac
EOF
chmod +x "$CASE/shim/curl"
run_installer "$RELEASE"
expect_failure "interrupted"
left=$(installed_files)
if [ "$left" = "llm-data-mcp" ] && [ "$(cat "$CASE/home/.local/bin/llm-data-mcp")" = old ]; then
    pass "interrupted: only the untouched previous llm-data-mcp remains"
else
    fail "interrupted: install dir holds: $(echo "$left" | tr '\n' ' ')"
fi

# --- 9. an interruption between staging and the rename -----------------------
# Both downloads are verified and staged under temp names in the install dir
# when the run is killed at the first rename. The staged files must be removed
# and the previous install must survive byte for byte.
new_case interrupted-staged
mkdir -p "$CASE/home/.local/bin"
printf 'old-cli' >"$CASE/home/.local/bin/llm-data"
printf 'old-mcp' >"$CASE/home/.local/bin/llm-data-mcp"
cat >"$CASE/shim/mv" <<'EOF'
#!/bin/sh
kill -TERM "$PPID"
sleep 5
exit 1
EOF
chmod +x "$CASE/shim/mv"
run_installer "$RELEASE"
expect_failure "interrupted at rename"
left=$(installed_files | tr '\n' ' ')
if [ "$left" = "llm-data llm-data-mcp " ] &&
    [ "$(cat "$CASE/home/.local/bin/llm-data")" = old-cli ] &&
    [ "$(cat "$CASE/home/.local/bin/llm-data-mcp")" = old-mcp ]; then
    pass "interrupted at rename: previous install intact, no staged files left"
else
    fail "interrupted at rename: install dir holds: $left"
fi

# --- 10/11. an unknown OS or arch exits non-zero with a message ---------------
# A fake uname reports a platform no release is built for.
for spec in "s:FreeBSD:operating system" "m:riscv64:architecture"; do
    flag=${spec%%:*}
    rest=${spec#*:}
    value=${rest%%:*}
    word=${rest#*:}
    new_case "unknown-$value"
    REAL_UNAME=$(command -v uname)
    cat >"$CASE/shim/uname" <<EOF
#!/bin/sh
if [ "\$1" = "-$flag" ]; then echo "$value"; else exec "$REAL_UNAME" "\$@"; fi
EOF
    chmod +x "$CASE/shim/uname"
    run_installer "$RELEASE"
    expect_failure "unknown $value"
    case "$OUT" in *"$value"*"$word"* | *"$word"*"$value"*) pass "unknown $value: message: $(echo "$OUT" | grep "$value" | head -1)" ;; *) fail "unknown $value: no message naming the $word: $OUT" ;; esac
    expect_nothing_installed "unknown $value"
done

echo
echo "$PASSED passed, $FAILED failed"
[ "$FAILED" -eq 0 ]
