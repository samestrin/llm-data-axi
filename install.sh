#!/bin/sh
# Installs the prebuilt llm-data and llm-data-mcp release binaries for this OS
# and architecture into ~/.local/bin. No clone, no Go toolchain, no sudo.
#
#   curl -fsSL https://raw.githubusercontent.com/samestrin/llm-data-axi/main/install.sh | sh
#
# Environment:
#   LLM_DATA_VERSION      release tag to install, e.g. v1.0.0 (default: latest)
#   LLM_DATA_INSTALL_DIR  where the binaries go (default: $HOME/.local/bin)
#   LLM_DATA_BASE_URL     where the release assets are fetched from; overrides
#                         LLM_DATA_VERSION. CI points it at a locally built
#                         release directory (file:///path/to/dist).
#
# To build from source instead, clone the repository and run `make install`.
#
# Every binary is checked against the release's SHA256SUMS.txt, and all of them
# are checked before any is installed: a tampered or truncated download fails
# the run and leaves the install directory as it was.
set -eu

REPO="samestrin/llm-data-axi"
BINARIES="llm-data llm-data-mcp"

err() {
    echo "install.sh: $*" >&2
    exit 1
}

case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *)
        err "unsupported operating system: $(uname -s). Prebuilt binaries exist for Linux and macOS; on Windows download the .exe from https://github.com/$REPO/releases, or build from source with \`make install\`."
        ;;
esac

case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *)
        err "unsupported architecture: $(uname -m). Prebuilt binaries exist for amd64 and arm64; build from source with \`make install\`."
        ;;
esac

if [ -n "${LLM_DATA_BASE_URL:-}" ]; then
    base_url=${LLM_DATA_BASE_URL%/}
elif [ -n "${LLM_DATA_VERSION:-}" ]; then
    base_url="https://github.com/$REPO/releases/download/$LLM_DATA_VERSION"
else
    base_url="https://github.com/$REPO/releases/latest/download"
fi

[ -n "${HOME:-}" ] || [ -n "${LLM_DATA_INSTALL_DIR:-}" ] || err "HOME is not set; set LLM_DATA_INSTALL_DIR"
install_dir=${LLM_DATA_INSTALL_DIR:-$HOME/.local/bin}

command -v curl >/dev/null 2>&1 || err "curl is required"
if command -v sha256sum >/dev/null 2>&1; then
    sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
    sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
    err "sha256sum or shasum is required to verify the download"
fi

# Downloads go to a private temp directory, and each binary is copied to a temp
# name inside the install directory before a rename puts it in place. A rename
# within one directory is atomic, so an interrupted run leaves either the old
# binary or the new one, never half of one. The trap removes both kinds of
# temp file however the script ends.
tmp=$(mktemp -d)
staged=""
cleanup() {
    rm -rf "$tmp"
    for f in $staged; do rm -f "$f"; done
}
trap cleanup EXIT
trap 'cleanup; trap - EXIT; exit 130' INT
trap 'cleanup; trap - EXIT; exit 143' TERM HUP

# --fail turns an HTTP error into a non-zero exit instead of saving the error
# page; the timeouts stop a stalled connection from hanging the install.
fetch() {
    curl --fail --silent --show-error --location \
        --connect-timeout 15 --max-time 300 \
        -o "$2" "$1" || err "download failed: $1"
}

echo "Installing llm-data for $os/$arch from $base_url"

fetch "$base_url/SHA256SUMS.txt" "$tmp/SHA256SUMS.txt"

for bin in $BINARIES; do
    asset="$bin-$os-$arch"
    fetch "$base_url/$asset" "$tmp/$asset"
    # sha256sum writes "<hash>  <name>", or "<hash> *<name>" in binary mode.
    want=$(awk -v name="$asset" '{ f = $2; sub(/^\*/, "", f); if (f == name) { print $1; exit } }' "$tmp/SHA256SUMS.txt")
    [ -n "$want" ] || err "$asset has no entry in SHA256SUMS.txt; refusing to install it"
    got=$(sha256 "$tmp/$asset")
    [ "$got" = "$want" ] || err "checksum mismatch for $asset (expected $want, got $got); nothing was installed"
done

mkdir -p "$install_dir"
for bin in $BINARIES; do
    part="$install_dir/.$bin.tmp.$$"
    staged="$staged $part"
    cp "$tmp/$bin-$os-$arch" "$part"
    chmod 755 "$part"
done
for bin in $BINARIES; do
    mv -f "$install_dir/.$bin.tmp.$$" "$install_dir/$bin"
done
staged=""

version=$("$install_dir/llm-data" --version 2>&1) || err "installed $install_dir/llm-data but it does not run: $version"
echo "Installed llm-data and llm-data-mcp to $install_dir"
echo "$version"

case ":$PATH:" in
    *":$install_dir:"*) ;;
    *) echo "Note: $install_dir is not on your PATH. Add it, e.g.: export PATH=\"$install_dir:\$PATH\"" ;;
esac
