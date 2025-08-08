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
  <a href="https://x.com/Pushchain">
    <img src="https://img.shields.io/badge/twitter-18a1d6.svg?style=flat-square" alt="twitter">
  </a>
  <a href="https://www.youtube.com/@pushprotocol">
    <img src="https://img.shields.io/badge/youtube-d95652.svg?style=flat-square&" alt="youtube">
  </a>
  <a href="./LICENSE" target="_blank">
    <img src="https://img.shields.io/badge/license-MIT-green.svg?style=flat-square" alt="license">
  </a>
</h4>

# Push Chain Node

Push Chain Node is the core implementation of Push Chain, a next-generation, shared-state Layer 1 blockchain. It powers universal app experiences by enabling seamless interoperability, universal execution, and a frictionless developer experience. The node software allows anyone to participate in the Push Chain network - running core validators, universal validators, or full nodes to help secure and operate the chain. Built for extensibility and performance, Push Chain Node is designed to connect any chain, any user, and any app, making cross-chain and cross-ecosystem applications possible out of the box.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Running Localnet](#running-localnet)
- [Directory Structure](#directory-structure)
- [Contributing](#contributing)

## Prerequisites

Before you begin, ensure you have the following installed:

- [Go 1.23+](https://golang.org/dl/)
- [Docker](https://www.docker.com/)
- [jq](https://stedolan.github.io/jq/download/) (for scripts)

## Running Localnet

Locknet is the local testnet environment for Push Chain. To spin up Locknet, use the following command:

```sh
git clone https://github.com/pushchain/push-chain-node.git
cd push-chain-node
make sh-testnet
```

## Directory Structure

- `app/` – Core application logic and configuration
- `x/` – Cosmos SDK modules (UExecutor, UTxVerifier, etc.)
- `precompiles/` – EVM precompiles for universal verification
- `proto/` – Protobuf definitions
- `cmd/` – CLI entrypoints
- `deploy/` – Deployment scripts and testnet configs
- `interchaintest/` – E2E and integration tests
- `utils/` – Utility functions

## Contributing

We welcome contributions from the community! To get started:

- Fork this repository and create a new branch for your feature or bugfix.
- Make your changes and ensure all tests pass (`go test ./... -v`).
- Open a pull request with a clear description of your changes.
- For major changes, please open an issue first to discuss what you would like to change.
