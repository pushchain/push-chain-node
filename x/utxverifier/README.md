# Universal Transaction Verification (UTxVerifier) Module

This is UTxVerifier (Universal Transaction Verification) module.

## Responsibilities

- Verifying transaction hashes of funds locked on source chains
- Performing RPC calls to external chains
- Storing verified transaction hashes for reference and validation

## Overview

The UTxVerifier module acts as the verification layer in a universal system, ensuring the authenticity of transactions before execution on the destination chain.