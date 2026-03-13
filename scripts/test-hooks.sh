#!/bin/bash
#
# Test Pre-Commit Hook
# Creates test scenarios to verify the hook is working correctly
#

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}  ContextHelp Pre-Commit Hook Test Suite${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

# Determine script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
HOOKS_DIR="$PROJECT_ROOT/.git/hooks"

# Check if hook is installed
if [ ! -f "$HOOKS_DIR/pre-commit" ]; then
    echo -e "${RED}✗ Pre-commit hook not found${NC}"
    echo "Run: ./scripts/install-hooks.sh"
    exit 1
fi

echo -e "${GREEN}✓${NC} Pre-commit hook found"
echo ""

# Create temporary test directory
TEST_DIR="$(mktemp -d)"
echo -e "Test directory: ${BLUE}$TEST_DIR${NC}"
echo ""

# Test counter
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Function to run a test
run_test() {
    local test_name=$1
    local test_file=$2
    local test_content=$3
    local should_fail=$4

    TESTS_RUN=$((TESTS_RUN + 1))

    echo -e "${BLUE}Test $TESTS_RUN: ${test_name}${NC}"

    # Create test file
    echo "$test_content" > "$TEST_DIR/$test_file"

    # Copy to project (but don't stage yet)
    cp "$TEST_DIR/$test_file" "$PROJECT_ROOT/$test_file"

    # Stage the file
    cd "$PROJECT_ROOT"
    git add "$test_file" 2>/dev/null || true

    # Run the hook
    if "$HOOKS_DIR/pre-commit" > "$TEST_DIR/hook_output.txt" 2>&1; then
        HOOK_RESULT=0
    else
        HOOK_RESULT=1
    fi

    # Check result
    if [ "$should_fail" = "true" ]; then
        if [ $HOOK_RESULT -eq 1 ]; then
            echo -e "${GREEN}✓${NC} Hook correctly rejected the file"
            TESTS_PASSED=$((TESTS_PASSED + 1))
        else
            echo -e "${RED}✗${NC} Hook should have rejected this file but didn't"
            TESTS_FAILED=$((TESTS_FAILED + 1))
        fi
    else
        if [ $HOOK_RESULT -eq 0 ]; then
            echo -e "${GREEN}✓${NC} Hook correctly allowed the file"
            TESTS_PASSED=$((TESTS_PASSED + 1))
        else
            echo -e "${RED}✗${NC} Hook should have allowed this file but didn't"
            cat "$TEST_DIR/hook_output.txt"
            TESTS_FAILED=$((TESTS_FAILED + 1))
        fi
    fi

    # Clean up
    git reset HEAD "$test_file" 2>/dev/null || true
    rm -f "$PROJECT_ROOT/$test_file"

    echo ""
}

# Run tests
echo -e "${BLUE}Running test scenarios...${NC}"
echo ""

# Test 1: Safe file with environment variable reference
run_test \
    "Safe config with env var" \
    "test-safe-config.yaml" \
    "api_key: \${OPENAI_API_KEY}" \
    "false"

# Test 2: Unsafe file with OpenAI key
run_test \
    "Config with OpenAI key (should fail)" \
    "test-unsafe-config.yaml" \
    "api_key: sk-proj-abcdefghijklmnopqrstuvwxyz1234567890" \
    "true"

# Test 3: Unsafe file with Anthropic key
run_test \
    "Config with Anthropic key (should fail)" \
    "test-anthropic.yaml" \
    "key: sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuv" \
    "true"

# Test 4: .env file (should fail - blocked file type)
run_test \
    ".env file (should fail)" \
    ".env" \
    "OPENAI_API_KEY=sk-proj-test123" \
    "true"

# Test 5: Safe markdown file
run_test \
    "Safe markdown documentation" \
    "test-docs.md" \
    "# Example\n\nUse \${OPENAI_API_KEY} for configuration." \
    "false"

# Test 6: File with AWS key
run_test \
    "File with AWS key (should fail)" \
    "test-aws.yaml" \
    "aws_access_key_id: AKIAIOSFODNN7EXAMPLE" \
    "true"

# Test 7: File with GitHub token
run_test \
    "File with GitHub token (should fail)" \
    "test-github.yaml" \
    "token: ghp_abcdefghijklmnopqrstuvwxyz123456" \
    "true"

# Test 8: Safe example file (should pass even with key pattern)
run_test \
    "Example file (should pass)" \
    "config.example.yaml" \
    "api_key: sk-proj-your-key-here" \
    "false"

# Summary
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}Test Results:${NC}"
echo -e "  Total:  $TESTS_RUN"
echo -e "  ${GREEN}Passed: $TESTS_PASSED${NC}"
echo -e "  ${RED}Failed: $TESTS_FAILED${NC}"
echo ""

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "${GREEN}✓ All tests passed!${NC}"
    echo ""
    echo "The pre-commit hook is working correctly."
    EXIT_CODE=0
else
    echo -e "${RED}✗ Some tests failed${NC}"
    echo ""
    echo "There may be issues with the pre-commit hook configuration."
    echo "Review the output above for details."
    EXIT_CODE=1
fi

# Clean up
rm -rf "$TEST_DIR"

exit $EXIT_CODE
