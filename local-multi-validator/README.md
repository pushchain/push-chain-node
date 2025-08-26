# Local Multi-Validator Setup

Docker-based local development environment for Push Chain with 3 core validators + 3 universal validators.

## Quick Start

```bash
./local-validator-manager start    # Start all validators
./local-validator-manager status   # Check status
./local-validator-manager stop     # Stop all validators
```

## Development

```bash
./local-validator-manager rebuild all  # Rebuild after code changes
./local-validator-manager dev-watch    # Auto-rebuild on file changes
```

## Network Access

**Core Validators:**
- Validator 1: `http://localhost:26657` (RPC), `http://localhost:1317` (REST)
- Validator 2: `http://localhost:26658` (RPC), `http://localhost:1318` (REST)  
- Validator 3: `http://localhost:26659` (RPC), `http://localhost:1319` (REST)

**Universal Validators:**
- Universal 1: `http://localhost:8080`
- Universal 2: `http://localhost:8081`
- Universal 3: `http://localhost:8082`

## Commands

Run `./local-validator-manager help` for all available commands.