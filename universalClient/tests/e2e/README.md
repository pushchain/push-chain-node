# E2E/Integration Tests for Universal Client

This directory contains end-to-end (E2E) and integration tests for the Universal Client. These tests verify the behavior of the system with real external dependencies and network connections.

## Test Organization

### Unit Tests vs E2E Tests

**Unit Tests** (in `*_test.go` files alongside source code):
- Test individual components in isolation
- Use mocks for external dependencies
- Fast execution
- No network calls
- No real blockchain connections

**E2E/Integration Tests** (in this directory):
- Test real interactions with external systems
- Use actual network connections
- Test full workflows across multiple components
- May require specific test environments

### Directory Structure

```
tests/e2e/
├── registry/     # Registry integration tests (real pchain node connections)
├── chains/       # Chain-specific integration tests
│   ├── evm/     # EVM chain integration tests (real RPC connections)
│   └── svm/     # Solana chain integration tests (real RPC connections)
└── core/        # Core functionality integration tests
```

## Running E2E Tests

### Prerequisites

1. **Test Environment Variables**:
   ```bash
   # Registry tests
   export PCHAIN_TEST_RPC_URL="http://localhost:26657"
   export PCHAIN_TEST_GRPC_URL="localhost:9090"
   
   # EVM tests
   export EVM_TEST_RPC_URL="https://eth-sepolia.g.alchemy.com/v2/YOUR_KEY"
   
   # Solana tests
   export SOLANA_TEST_RPC_URL="https://api.devnet.solana.com"
   ```

2. **Test Data Setup**:
   - Ensure test chain configurations exist in the registry
   - Have test tokens configured for integration tests

### Running Tests

```bash
# Run all e2e tests
go test ./tests/e2e/...

# Run specific test suites
go test ./tests/e2e/registry
go test ./tests/e2e/chains/evm
go test ./tests/e2e/chains/svm

# Run with verbose output
go test -v ./tests/e2e/...

# Run with specific timeout (e2e tests may take longer)
go test -timeout 5m ./tests/e2e/...
```

## Writing E2E Tests

### Guidelines

1. **Test Naming**: Use descriptive names that indicate the integration being tested
   ```go
   func TestRegistry_RealNodeConnection(t *testing.T)
   func TestEVMClient_MainnetForking(t *testing.T)
   ```

2. **Skip Conditions**: Skip tests when required environment is not available
   ```go
   func TestRegistry_RealConnection(t *testing.T) {
       if os.Getenv("PCHAIN_TEST_RPC_URL") == "" {
           t.Skip("PCHAIN_TEST_RPC_URL not set, skipping integration test")
       }
       // ... test implementation
   }
   ```

3. **Test Isolation**: Each test should be independent and not rely on state from other tests

4. **Cleanup**: Always clean up resources (connections, test data) after tests
   ```go
   defer client.Stop()
   defer cleanup()
   ```

5. **Timeouts**: Use appropriate timeouts for network operations
   ```go
   ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
   defer cancel()
   ```

### Example E2E Test

```go
package registry_test

import (
    "context"
    "os"
    "testing"
    "time"
    
    "github.com/stretchr/testify/require"
    "github.com/rollchains/pchain/universalClient/registry"
)

func TestRegistry_RealNodeConnection(t *testing.T) {
    // Skip if no test URL provided
    grpcURL := os.Getenv("PCHAIN_TEST_GRPC_URL")
    if grpcURL == "" {
        t.Skip("PCHAIN_TEST_GRPC_URL not set, skipping integration test")
    }
    
    // Create real client
    client, err := registry.NewRegistryClient([]string{grpcURL}, logger)
    require.NoError(t, err)
    defer client.Close()
    
    // Test real connection
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    configs, err := client.GetAllChainConfigs(ctx)
    require.NoError(t, err)
    require.NotEmpty(t, configs, "should have chain configurations")
    
    // Verify real data
    for _, config := range configs {
        require.NotEmpty(t, config.Chain)
        require.NotEmpty(t, config.VmType)
    }
}
```

## Test Categories

### Registry Integration Tests
- Real pchain node connections
- Configuration fetching
- Cache synchronization
- Multi-node failover

### EVM Integration Tests
- Real RPC connections
- Block fetching
- Transaction submission
- Event listening

### Solana Integration Tests
- Real RPC connections
- Slot fetching
- Account queries
- Transaction monitoring

### Core Integration Tests
- Full update cycles
- Multi-chain coordination
- Error recovery scenarios

## CI/CD Considerations

- E2E tests should run in a separate CI job
- Use test networks (testnets) not mainnet
- Consider using Docker containers for test infrastructure
- Set appropriate timeouts for CI environments
- Mark E2E tests with build tags if needed:
  ```go
  //go:build e2e
  // +build e2e
  ```

## Troubleshooting

Common issues and solutions:

1. **Connection timeouts**: Increase timeout values or check network connectivity
2. **Missing configurations**: Ensure test data is properly set up in the registry
3. **Rate limiting**: Add delays between tests or use dedicated test endpoints
4. **Flaky tests**: Add retries for transient network issues

## Future Enhancements

- Add performance benchmarks
- Create test fixtures for common scenarios
- Implement test data generators
- Add integration with monitoring tools