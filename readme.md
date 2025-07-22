<h1 align="center">
    <a href="https://push.org/#gh-light-mode-only">
    <img width='20%' height='10%' src="https://res.cloudinary.com/dx8mqtt0p/image/upload/t_pushchain_logo/Screenshot_2025-07-15_at_11.35.04_AM_wxoldu">
    </a>
    <a href="https://push.org/#gh-dark-mode-only">
    <img width='20%' height='10%' src="https://res.cloudinary.com/dx8mqtt0p/image/upload/t_pushchain_logo/Screenshot_2025-07-15_at_11.35.04_AM_wxoldu">
    </a>
</h1>

<p align="center">
  <i>Push protocol is evolving to Push Chain, a shared-state L1 designed to deliver universal app experiences (Any Chain. Any User. Any App). 🚀</i>
</p>

<h4 align="center">
  <a href="https://discord.com/invite/pushprotocol">
    <img src="https://img.shields.io/badge/discord-7289da.svg?style=flat-square" alt="discord">
  </a>
  <a href="https://twitter.com/pushprotocol">
    <img src="https://img.shields.io/badge/twitter-18a1d6.svg?style=flat-square" alt="twitter">
  </a>
  <a href="https://www.youtube.com/@pushprotocol">
    <img src="https://img.shields.io/badge/youtube-d95652.svg?style=flat-square&" alt="youtube">
  </a>
  <a href="https://img.shields.io/badge/license-MIT-green.svg?style=flat-square" target="_blank">
    <img src="https://img.shields.io/badge/license-MIT-green.svg?style=flat-square" alt="license">
  </a>
</h4>

---

# Push Chain

Push Chain is a next-generation, shared-state Layer 1 blockchain designed to deliver universal app experiences across any chain, for any user, and any app.Push Chain enables seamless interoperability, universal execution, and a frictionless developer experience.

- **Website:** [push.org](https://push.org)
- **Docs:** _Coming soon_
- **Testnet:** [Join Discord](https://discord.com/invite/pushprotocol)

## Table of Contents
- [Features](#features)
- [Quick Start](#quick-start)
- [Development](#development)
- [Testnet](#testnet)
- [Directory Structure](#directory-structure)
- [Resources](#resources)
- [License](#license)

---

## Features

- **Universal App Experience:** Build apps that work across any chain, for any user, with a single universal account and payload model.
- **Shared-State L1:** All apps and users share a single, composable state, enabling new cross-chain UX and programmability.
- **Universal Executor (UE):** Execute actions originating from any source chain, supporting EVM, Solana, Cosmos, and more.
- **Universal Transaction Verification (UTV):** Securely verify and relay transactions from external chains.
- **EVM & CosmWasm Support:** Native EVM and CosmWasm smart contracts, with IBC and Token Factory modules.
- **Interoperability:** Built-in IBC, cross-chain token transfers, and universal chain configuration.
- **Developer Friendly:** Modern tooling, code generation, and modular architecture.

---

## Quick Start

### Prerequisites
- [Go 1.23+](https://golang.org/dl/)
- [Docker](https://www.docker.com/)
- [jq](https://stedolan.github.io/jq/download/) (for scripts)

### Run with Docker
```sh
docker-compose up
```

### Build & Run Locally
```sh
git clone https://github.com/push-protocol/push-chain.git
cd push-chain
make install
pchaind start --home ~/.pchain
```

### Join Testnet
See [Testnet](#testnet) section or join our [Discord](https://discord.com/invite/pushprotocol) for the latest instructions.

---

## Development

### Build the Binary
```sh
make install
```

### Generate Protobuf & Go Code
```sh
make proto-gen
```

### Run Tests
```sh
go test ./... -v
```

### Webapp Template
Generate a webapp template:
```sh
make generate-webapp
```

---

## Testnet

Spin up a local testnet node:
```sh
make sh-testnet
```

Or use the provided scripts in `deploy/` and `scripts/` for advanced setups. See [deploy/readme.md](deploy/readme.md) for detailed instructions on running multi-node or remote testnets.

---

## Directory Structure

- `app/`         – Core application logic and configuration
- `x/`           – Cosmos SDK modules (UE, UTV, etc.)
- `precompiles/` – EVM precompiles for universal verification
- `proto/`       – Protobuf definitions
- `cmd/`         – CLI entrypoints
- `deploy/`      – Deployment scripts and testnet configs
- `interchaintest/` – E2E and integration tests
- `utils/`       – Utility functions

---

## Resources
- [Website](https://push.org)
- [Discord](https://discord.com/invite/pushprotocol)
- [Twitter](https://twitter.com/pushprotocol)
- [YouTube](https://www.youtube.com/@pushprotocol)
- [Docs (coming soon)](https://push.org)

---

## Contributing

We welcome contributions from the community! To get started:

- Fork this repository and create a new branch for your feature or bugfix.
- Make your changes and ensure all tests pass (`go test ./... -v`).
- Open a pull request with a clear description of your changes.
- For major changes, please open an issue first to discuss what you would like to change.

For more details, see our [CONTRIBUTING.md](CONTRIBUTING.md) (if available) or reach out on [Discord](https://discord.com/invite/pushprotocol).

---

## License

[MIT](LICENSE)
