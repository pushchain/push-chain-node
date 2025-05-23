# Push Chain Crosschain Module Testing

## Overview
Unified testing framework for the Push Chain crosschain module with comprehensive validation tests and beautiful HTML reports.

## Prerequisites
Install the required testing libraries:
```bash
# Install HTML report generators
go install github.com/vakenbolt/go-test-report@latest
go install github.com/jstemmer/go-junit-report@latest
```

## Available Commands

### Primary Commands
```bash
make -f Makefile.testing test           # Run all crosschain tests
make -f Makefile.testing test-html      # Generate interactive HTML report  
make -f Makefile.testing test-coverage  # Run tests with coverage analysis
make -f Makefile.testing test-all       # Complete test suite + all reports
```

### Utility Commands  
```bash
make -f Makefile.testing test-quick     # Quick test execution
make -f Makefile.testing clean          # Clean generated files
make -f Makefile.testing help           # Show available commands
```

## Generated Reports

| Report | File Location | Purpose |
|--------|---------------|---------|
| **Interactive Tests** | `x/crosschain/test_report.html` | Main test results with detailed output |
| **Code Coverage** | `x/crosschain/coverage.html` | Coverage analysis and visualization |

## Test Coverage

The framework includes comprehensive validation tests for:

- **Message Validation**: All crosschain message types (MsgUpdateParams, MsgDeployNMSC, MsgMintPush, MsgExecutePayload)
- **Input Validation**: Edge cases, malformed inputs, injection attempts
- **Address Validation**: EVM addresses, format checking, cross-chain compatibility  
- **Authorization**: Signer validation, permission checks
- **EVM Integration**: Factory calls, gas calculations, fee handling
- **CLI Commands**: Command structure, argument validation

## Quick Start

```bash
# Run all tests and generate reports
make -f Makefile.testing test-all

# View results
open x/crosschain/test_report.html     # Interactive test report
open x/crosschain/coverage.html       # Code coverage
```

Reports are git-ignored and safe for automated environments.