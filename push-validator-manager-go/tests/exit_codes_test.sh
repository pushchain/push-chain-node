#!/usr/bin/env bash
# Exit code integration tests for push-validator-manager
# Tests all standard exit codes defined in internal/exitcodes/codes.go

set -euo pipefail

MANAGER="${MANAGER:-./push-validator-manager}"
FAILED=0
PASSED=0

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

pass() {
    echo -e "${GREEN}✓${NC} $*"
    ((PASSED++))
}

fail() {
    echo -e "${RED}✗${NC} $*"
    ((FAILED++))
}

assert_exit() {
    local expected=$1
    shift
    local desc="$1"
    shift

    set +e
    "$@" >/dev/null 2>&1
    local actual=$?
    set -e

    if [[ $actual -eq $expected ]]; then
        pass "$desc (exit $actual)"
    else
        fail "$desc (expected $expected, got $actual)"
    fi
}

echo "Testing Exit Codes for push-validator-manager"
echo "=============================================="
echo

# Exit code 0: Success
echo "Success (0):"
assert_exit 0 "help command" "$MANAGER" --help

# Exit code 2: InvalidArgs
echo
echo "Invalid Arguments (2):"
assert_exit 2 "unknown flag" "$MANAGER" --invalid-flag
assert_exit 2 "unknown command" "$MANAGER" nonexistent-command

# Exit code 3: PreconditionFailed (requires specific setup)
echo
echo "Precondition Failed (3):"
# These would require specific environment setup to test properly
echo "  ⊘ Skipping precondition tests (require environment setup)"

# Exit code 4: NetworkError (requires network conditions)
echo
echo "Network Error (4):"
# Test unreachable RPC endpoint
assert_exit 4 "unreachable RPC" "$MANAGER" status --rpc http://unreachable-host-xyz:9999

# Exit code 5: ProcessError (requires process management context)
echo
echo "Process Error (5):"
echo "  ⊘ Skipping process error tests (require specific process state)"

# Exit code 6: ValidationError (requires validation scenarios)
echo
echo "Validation Error (6):"
echo "  ⊘ Skipping validation tests (require specific validation scenarios)"

# Summary
echo
echo "=============================================="
echo "Test Results: $PASSED passed, $FAILED failed"
echo "=============================================="

if [[ $FAILED -gt 0 ]]; then
    exit 1
fi

exit 0
