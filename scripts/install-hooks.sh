#!/bin/bash
# Install git hooks
# 安装 git hooks

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
HOOKS_DIR="$REPO_ROOT/.git/hooks"

# Ensure hooks directory exists
mkdir -p "$HOOKS_DIR"

# Install pre-commit hook
cp "$SCRIPT_DIR/pre-commit.sh" "$HOOKS_DIR/pre-commit"
chmod +x "$HOOKS_DIR/pre-commit"

echo "✓ Pre-commit hook installed successfully"
echo "  Location: $HOOKS_DIR/pre-commit"
