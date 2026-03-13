#!/bin/bash
#
# Install Git Hooks for ContextHelp
# This script installs pre-commit hooks for secret detection
#

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}  ContextHelp Git Hooks Installer${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

# Determine script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
HOOKS_DIR="$PROJECT_ROOT/.git/hooks"

echo -e "Project root: ${BLUE}$PROJECT_ROOT${NC}"
echo -e "Hooks directory: ${BLUE}$HOOKS_DIR${NC}"
echo ""

# Check if we're in a git repository
if [ ! -d "$PROJECT_ROOT/.git" ]; then
    echo -e "${RED}Error: Not in a git repository${NC}"
    echo "Please run this script from the ContextHelp project root."
    exit 1
fi

# Check for gitleaks
echo -e "${BLUE}[1/4]${NC} Checking for gitleaks..."
if command -v gitleaks &> /dev/null; then
    echo -e "${GREEN}✓${NC} gitleaks found: $(gitleaks version)"
else
    echo -e "${YELLOW}⚠️  gitleaks not found${NC}"
    echo ""
    echo "gitleaks is recommended for secret scanning."
    echo "Install it with:"
    echo ""
    echo "  macOS:     brew install gitleaks"
    echo "  Linux:     wget https://github.com/gitleaks/gitleaks/releases/download/v8.18.1/gitleaks_8.18.1_linux_x64.tar.gz"
    echo "  Windows:   choco install gitleaks"
    echo ""
    read -p "Continue without gitleaks? [y/N] " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi
echo ""

# Install pre-commit hook
echo -e "${BLUE}[2/4]${NC} Installing pre-commit hook..."

# Backup existing hook if present
if [ -f "$HOOKS_DIR/pre-commit" ]; then
    echo -e "${YELLOW}⚠️  Existing pre-commit hook found${NC}"
    BACKUP_FILE="$HOOKS_DIR/pre-commit.backup.$(date +%Y%m%d_%H%M%S)"
    cp "$HOOKS_DIR/pre-commit" "$BACKUP_FILE"
    echo -e "   Backed up to: ${BACKUP_FILE}"
fi

# Copy pre-commit hook
cp "$HOOKS_DIR/pre-commit" "$HOOKS_DIR/pre-commit" 2>/dev/null || true
chmod +x "$HOOKS_DIR/pre-commit"
echo -e "${GREEN}✓${NC} Pre-commit hook installed"
echo ""

# Verify .gitleaks.toml exists
echo -e "${BLUE}[3/4]${NC} Verifying gitleaks configuration..."
if [ -f "$PROJECT_ROOT/.gitleaks.toml" ]; then
    echo -e "${GREEN}✓${NC} .gitleaks.toml found"
else
    echo -e "${YELLOW}⚠️  .gitleaks.toml not found${NC}"
    echo "   Creating default configuration..."
    # The file should already exist, but this is a fallback
    cat > "$PROJECT_ROOT/.gitleaks.toml" << 'EOF'
title = "ContextHelp Gitleaks Configuration"
[extend]
useDefault = true
EOF
    echo -e "${GREEN}✓${NC} Created .gitleaks.toml"
fi
echo ""

# Verify .gitignore includes sensitive files
echo -e "${BLUE}[4/4]${NC} Verifying .gitignore..."
if [ -f "$PROJECT_ROOT/.gitignore" ]; then
    echo -e "${GREEN}✓${NC} .gitignore found"

    # Check for critical patterns
    MISSING_PATTERNS=()

    if ! grep -q "^\.env$" "$PROJECT_ROOT/.gitignore"; then
        MISSING_PATTERNS+=(".env")
    fi

    if ! grep -q "^\*\.key$\|^.*\.key$" "$PROJECT_ROOT/.gitignore"; then
        MISSING_PATTERNS+=("*.key")
    fi

    if ! grep -q "^\*\.token$\|^.*\.token$" "$PROJECT_ROOT/.gitignore"; then
        MISSING_PATTERNS+=("*.token")
    fi

    if [ ${#MISSING_PATTERNS[@]} -gt 0 ]; then
        echo -e "${YELLOW}⚠️  Some patterns missing from .gitignore:${NC}"
        for pattern in "${MISSING_PATTERNS[@]}"; do
            echo "   - $pattern"
        done
        echo ""
        echo "Consider adding these patterns to .gitignore"
    fi
else
    echo -e "${RED}✗${NC} .gitignore not found!"
    echo "   Create .gitignore with sensitive file patterns"
fi
echo ""

# Test the hook
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}✓ Installation complete!${NC}"
echo ""
echo "The pre-commit hook will now run automatically before each commit."
echo ""
echo "To test the hook manually:"
echo "  $HOOKS_DIR/pre-commit"
echo ""
echo "To skip the hook temporarily (NOT recommended):"
echo "  git commit --no-verify -m 'message'"
echo ""
echo "To skip specific checks:"
echo "  SKIP_GITLEAKS=1 git commit -m 'message'"
echo "  SKIP_PATTERNS=1 git commit -m 'message'"
echo "  SKIP_FILE_CHECK=1 git commit -m 'message'"
echo "  SKIP_YAML_CHECK=1 git commit -m 'message'"
echo ""
echo "For more information, see:"
echo "  • SECURITY.md - Security best practices"
echo "  • .gitleaks.toml - Gitleaks configuration"
echo ""
