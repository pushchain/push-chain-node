# Push Chain Validator Setup - TODO List

## High Priority
- [ ] Update pchaind binary source to download from GitHub releases instead of local mount
  - Currently mounting from: `../build/pchaind`
  - Need to use GitHub releases URL
  - Remove local binary mount from docker-compose.yml


## Future Enhancements
- [ ] Add nginx reverse proxy as part of Docker Compose setup for public validator access
  - Include nginx service in docker-compose.yml
  - Automated SSL certificate management with Let's Encrypt
  - Configuration templates for all endpoints (RPC, EVM, API, gRPC)
  - Optional profile for enabling/disabling public access
- [ ] Remove IPs and use URLS
