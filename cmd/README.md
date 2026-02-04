# Command Packages (`cmd`)

This directory contains the entry points for the different command-line applications
provided by the project. Each subdirectory under `cmd` represents a standalone binary
with its own `main` package.

The commands are responsible for:
- Parsing CLI arguments and flags
- Initializing configuration
- Bootstrapping the required services
- Invoking the core application logic



# 📦 Available Commands

### `pchaind`

`pchaind` is the primary chain daemon for the push-chain node.

Responsibilities include:
- Starting and managing the blockchain node
- Initializing networking, consensus, and state components
- Handling node lifecycle and runtime configuration
- Acting as the main long-running process for the chain


### `puniversald`

`puniversald` provides a universal daemon interface for interacting with the push-chain
ecosystem.

Responsibilities include:
- Exposing common functionality across different environments
- Handling auxiliary or supporting services
- Providing a flexible entry point for integrations or tooling



## 🧭 Design Notes

- Each command under `cmd` builds into a separate binary.
- Business logic is intentionally kept outside of `cmd` to keep command definitions thin.
- Shared logic should live in reusable packages rather than inside command directories.

This structure helps keep the codebase modular, testable, and easy for new contributors
to understand.
