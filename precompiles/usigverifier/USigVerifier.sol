// SPDX-License-Identifier: MIT
pragma solidity >=0.8.18;

/// @dev The USigVerifier contract's address (legacy).
address constant USigVerifier_PRECOMPILE_ADDRESS = 0x00000000000000000000000000000000000000ca;

/// @dev The USigVerifier contract's new address in the reserved Push precompile range.
address constant USigVerifier_PRECOMPILE_ADDRESS_V2 = 0xEC00000000000000000000000000000000000001;

/// @dev The IUSigVerifier contract's instance (legacy address).
IUSigVerifier constant USigVerifier_CONTRACT = IUSigVerifier(USigVerifier_PRECOMPILE_ADDRESS);

/// @dev The IUSigVerifier contract's instance (new address).
IUSigVerifier constant USigVerifier_CONTRACT_V2 = IUSigVerifier(USigVerifier_PRECOMPILE_ADDRESS_V2);

/// @dev The IUSigVerifier contract's interface.
interface IUSigVerifier {
    /// @notice Verifies an Ed25519 signature over the ASCII hex form of msgDigest.
    /// @dev The signature MUST be produced over the 66-byte UTF-8 sequence
    ///      `"0x" + hex(msgDigest)`, NOT over the raw 32 bytes of msgDigest.
    ///      This convention exists so Solana wallets (Phantom, Solflare, etc.)
    ///      display a human-readable hex string in their sign-message prompt.
    ///      For raw-bytes semantics, use {verifyEd25519RawMessage}.
    /// @param pubKey 32-byte Ed25519 public key (a Solana address is exactly this).
    /// @param msgDigest The 32-byte digest. Off-chain signer must sign `"0x" + hex(msgDigest)` (66 bytes).
    /// @param signature 64-byte Ed25519 signature.
    /// @return isValid True iff signature is valid for (pubKey, "0x"+hex(msgDigest)).
    function verifyEd25519(bytes calldata pubKey, bytes32 msgDigest, bytes calldata signature) external view returns (bool);

    /// @notice Verifies an Ed25519 signature over raw message bytes.
    /// @dev Standard Ed25519 verification: signature is checked against the raw
    ///      bytes of `message`. Use this when your off-chain signer uses the
    ///      conventional `ed25519.Sign(privKey, rawBytes)` API (the default in
    ///      every Solana SDK / nacl library).
    /// @param pubKey 32-byte Ed25519 public key.
    /// @param message Raw message bytes that were signed (any length).
    /// @param signature 64-byte Ed25519 signature.
    /// @return isValid True iff signature is valid for (pubKey, message).
    function verifyEd25519RawMessage(bytes calldata pubKey, bytes calldata message, bytes calldata signature) external view returns (bool);
}
