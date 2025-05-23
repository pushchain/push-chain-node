# Push Chain Crosschain Module Testing

## Overview
Unified testing framework for the Push Chain crosschain module with comprehensive validation tests and beautiful HTML reports.

## Quick Start

### 1. Automatic Setup (Run Once)
```bash
make -f Makefile.testing setup
```
This automatically installs all required testing libraries:
- `go-test-report` - Beautiful HTML test reports
- `go-junit-report` - JUnit XML report generation  
- `junit2html` - HTML conversion utility

### 2. Run Tests
```bash
make -f Makefile.testing test-all    # Complete test suite + reports
```

## Available Commands

### Primary Commands
```bash
make -f Makefile.testing setup       # Install libraries (run once)
make -f Makefile.testing test        # Run all crosschain tests
make -f Makefile.testing test-html   # Generate interactive HTML report  
make -f Makefile.testing test-coverage # Run tests with coverage analysis
make -f Makefile.testing test-all    # Complete test suite + all reports
```

### Utility Commands
```bash
make -f Makefile.testing test-quick  # Quick test execution
make -f Makefile.testing clean       # Clean generated files
make -f Makefile.testing help        # Show available commands
```

## Generated Reports

After running tests, you'll find:
- `x/crosschain/test_report.html` - Interactive test results with detailed failure analysis
- `x/crosschain/coverage.html` - Code coverage analysis

Open these files in your browser to view the results.

## Test Coverage

The framework includes 85+ comprehensive test cases across:
- **Message Validation**: All 5 crosschain message types
- **Address Validation**: EVM address format checking  
- **Input Validation**: Malicious input detection
- **CLI Commands**: Transaction and query command validation
- **Module Lifecycle**: Genesis, codec, and GRPC testing
- **EVM Integration**: Factory and NMSC contract interactions
- **Fee Calculations**: Gas cost and fee deduction testing

## What The Tests Find

The tests detect real validation vulnerabilities including:
- Invalid address formats and zero addresses
- Missing input validation on transaction hashes
- Cross-chain format inconsistencies
- Injection attack vectors
- Authorization bypass attempts

## CI/CD Integration

Add to your CI pipeline:
```yaml
- name: Setup Test Environment
  run: make -f Makefile.testing setup

- name: Run Comprehensive Tests
  run: make -f Makefile.testing test-all
```

## Files Created

All generated files are automatically excluded from git via `.gitignore` patterns.