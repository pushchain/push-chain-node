# Push Validator Manager Tests

## Exit Code Tests

The `exit_codes_test.sh` script validates that the CLI returns proper exit codes as defined in `internal/exitcodes/codes.go`.

### Exit Code Standards

| Code | Meaning | Example |
|------|---------|---------|
| 0 | Success | `push-validator --help` |
| 1 | General error | Unhandled exceptions |
| 2 | Invalid arguments | `push-validator --invalid-flag` |
| 3 | Precondition failed | Not initialized, missing config |
| 4 | Network error | RPC unreachable, timeout |
| 5 | Process error | Failed to start/stop node |
| 6 | Validation error | Invalid config, corrupted data |

### Running Tests

```bash
# Build the binary first
make build

# Run exit code tests
MANAGER=./build/push-validator ./tests/exit_codes_test.sh
```

### CI Integration

Add to your CI pipeline (GitHub Actions example):

```yaml
- name: Test Exit Codes
  run: |
    make build
    MANAGER=./build/push-validator ./tests/exit_codes_test.sh
```

### Non-Interactive Install Test

To test that non-interactive installation exits with code 0 on success:

```bash
# Should exit 0 on successful install
RESET_DATA=yes AUTO_START=no bash install.sh --use-local --no-start

# Check exit code
echo $?  # Should be 0
```

### Coverage

Current test coverage:
- ✅ Exit 0: Help command
- ✅ Exit 2: Invalid flag, unknown command
- ✅ Exit 4: Unreachable RPC endpoint
- ⊘ Exit 3: Requires environment setup (manual testing)
- ⊘ Exit 5: Requires process state (manual testing)
- ⊘ Exit 6: Requires validation scenarios (manual testing)

### Future Improvements

- Add integration tests for exit codes 3, 5, 6 with proper test fixtures
- Add CI job to run exit code tests on every PR
- Add install.sh exit code validation in CI
