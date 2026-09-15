#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

INSTALL_DIR="/usr/local/bin"
BUILD_DIR="build"

# Binaries to build and install. The CLI must come first: the MCP server
# resolves the CLI binary at startup (see internal/data/mcpserver).
BINARIES=(
    "llm-data"
    "llm-data-mcp"
)

VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
# The CLI version lives on commands.Version, not on main: the cobra root reads
# it from there, so stamping main would leave `llm-data --version` reporting the
# hardcoded default.
CLI_LDFLAGS="-s -w -X github.com/samestrin/llm-data-axi/internal/data/commands.Version=$VERSION"
MCP_LDFLAGS="-s -w -X main.serverVersion=$VERSION"

echo -e "${YELLOW}llm-data installer${NC}"
echo "======================"

# Check if running as root (needed for /usr/local/bin)
if [[ $EUID -ne 0 ]]; then
    echo -e "${RED}Error: This script must be run with sudo${NC}"
    echo "Usage: sudo ./install.sh"
    exit 1
fi

# Get the directory where the script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Check for go
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: go is not installed${NC}"
    exit 1
fi

echo -e "${GREEN}Building binaries...${NC}"
mkdir -p "$BUILD_DIR"

echo "  Building llm-data..."
go build -ldflags "$CLI_LDFLAGS" -o "$BUILD_DIR/llm-data" ./cmd/llm-data
echo "  Building llm-data-mcp..."
go build -ldflags "$MCP_LDFLAGS" -o "$BUILD_DIR/llm-data-mcp" ./cmd/llm-data-mcp

echo -e "${GREEN}Installing to $INSTALL_DIR...${NC}"
for bin in "${BINARIES[@]}"; do
    echo "  Installing $bin..."
    # Clear xattrs on source before copying
    xattr -cr "$BUILD_DIR/$bin" 2>/dev/null || true
    # Remove old binary first
    rm -f "$INSTALL_DIR/$bin"
    # Copy binary
    cp "$BUILD_DIR/$bin" "$INSTALL_DIR/$bin"
    # Clear all extended attributes recursively
    xattr -cr "$INSTALL_DIR/$bin" 2>/dev/null || true
    # Re-sign with ad-hoc signature (required for Apple Silicon)
    codesign --force --sign - "$INSTALL_DIR/$bin" 2>/dev/null || true
    chmod 755 "$INSTALL_DIR/$bin"
done

echo ""
echo -e "${GREEN}Installation complete!${NC}"
echo "Installed ${#BINARIES[@]} binaries to $INSTALL_DIR"

# Verify installation
echo ""
echo "Verifying installation..."
FAILED=0
for bin in "${BINARIES[@]}"; do
    if [[ -x "$INSTALL_DIR/$bin" ]]; then
        # MCP binaries need stdin, so just check they start without being killed
        if [[ "$bin" == *"-mcp" ]]; then
            # Send empty input and check it doesn't get killed (exit 137 = killed)
            echo "" | timeout 2 "$INSTALL_DIR/$bin" >/dev/null 2>&1
            EXIT_CODE=$?
            if [[ $EXIT_CODE -eq 137 || $EXIT_CODE -eq 143 ]]; then
                echo -e "  ${RED}✗${NC} $bin (killed by macOS)"
                FAILED=1
            else
                echo -e "  ${GREEN}✓${NC} $bin"
            fi
        else
            # CLI binaries - try --version first, fall back to --help
            if timeout 2 "$INSTALL_DIR/$bin" --version >/dev/null 2>&1 || \
               timeout 2 "$INSTALL_DIR/$bin" --help >/dev/null 2>&1; then
                echo -e "  ${GREEN}✓${NC} $bin"
            else
                echo -e "  ${RED}✗${NC} $bin"
                FAILED=1
            fi
        fi
    else
        echo -e "  ${RED}✗${NC} $bin (not found)"
        FAILED=1
    fi
done

if [[ $FAILED -eq 1 ]]; then
    echo -e "${RED}Some binaries failed to install${NC}"
    exit 1
fi

echo ""
echo -e "${GREEN}All binaries installed successfully!${NC}"
