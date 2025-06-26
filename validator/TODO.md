# Push Chain Validator Setup - TODO List

## High Priority
- [ ] Change genesis URL from local file mount to download from hosted location
  - Currently using: `file:///genesis/genesis.json` 
  - Need to update to hosted URL once available
  - Update networks.json with the new URL

- [ ] Update pchaind binary source to download from GitHub releases instead of local mount
  - Currently mounting from: `../build/pchaind`
  - Need to use GitHub releases URL
  - Remove local binary mount from docker-compose.yml

## Completed
- [x] Remove network selection (only testnet exists)
- [x] Use correct RPC URLs (https://evm.rpc-testnet-donut-node1.push.org)
- [x] Update explorer URL (https://donut.push.network/)
- [x] Update faucet URL (https://faucet.push.org/)
- [x] Simplify setup to only ask for validator name
- [x] Fix tini binary path (/usr/bin/tini)
- [x] Fix genesis file loading with local mount
- [x] Add keyring backend support for test mode
- [x] Remove docker-compose version warning

## Future Enhancements
- [ ] Add nginx reverse proxy as part of Docker Compose setup for public validator access
  - Include nginx service in docker-compose.yml
  - Automated SSL certificate management with Let's Encrypt
  - Configuration templates for all endpoints (RPC, EVM, API, gRPC)
  - Optional profile for enabling/disabling public access
- [ ] Add mainnet support when available
- [ ] Add automated validator registration after funding
- [ ] Add monitoring and alerting setup
- [ ] Add backup/restore functionality
- [ ] Add validator key import/export utilities
- [ ] Add automated updates mechanism
- [ ] Create validator management CLI commands
- [ ] Add multi-validator support