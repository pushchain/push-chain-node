# Universal Transaction Verification (UTV) Module

This is a UTV (Universal Transaction Verification) module, built using [`spawn`](https://github.com/rollchains/spawn).

## Responsibilities

- Verifying transaction hashes of funds locked on source chains
- Performing RPC calls to external chains
- Storing verified transaction hashes for reference and validation

## Overview

The UTV module acts as the verification layer in a cross-chain system, ensuring the authenticity of transactions before execution on the destination chain.