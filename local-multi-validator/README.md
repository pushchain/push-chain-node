# Local Multi-Validator Setup

Docker-based local development environment for Push Chain with 3 core validators + 3 universal validators.

## Quick Start

```bash
# Start all validators
./local-validator-manager start

# Check status
./local-validator-manager status

# View logs
./local-validator-manager logs core-validator-1

# Stop all
./local-validator-manager stop
```

## Available Commands

Run `./local-validator-manager help` for complete command reference including:

- **Setup**: start, stop, restart, status
- **Validator Management**: validator-status, logs, restart-validator, validators
- **Maintenance**: regenerate-accounts, clean-reset
- **Rebuild**: rebuild all/core/universal/clean for development

## Network Access

### Core Validators
- **Validator 1**: RPC `http://localhost:26657`, REST `http://localhost:1317`, WS `ws://localhost:8546`
- **Validator 2**: RPC `http://localhost:26658`, REST `http://localhost:1318`, WS `ws://localhost:8548`
- **Validator 3**: RPC `http://localhost:26659`, REST `http://localhost:1319`, WS `ws://localhost:8550`

### Universal Validators
- **Universal 1**: `http://localhost:8080`
- **Universal 2**: `http://localhost:8081`
- **Universal 3**: `http://localhost:8082`

## Development Workflow

```bash
# Make code changes, then:
./local-validator-manager rebuild all    # Fast rebuild with cache
./local-validator-manager restart        # Apply changes

# Or rebuild specific components:
./local-validator-manager rebuild core
./local-validator-manager rebuild universal
```