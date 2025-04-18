// SPDX-License-Identifier: MIT
pragma solidity >=0.8.18;

/// @dev The ISolVerifier contract's address.
address constant ISOLVERIFIER_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000000902;

/// @dev The ISolVerifier contract's instance.
ISolVerifier constant ISolVerifier_CONTRACT = ISolVerifier(ISOLVERIFIER_PRECOMPILE_ADDRESS);

/// @dev The ISolVerifier contract's interface.
interface ISolVerifier {
    /// @notice Verifies a signature using Ed25519
    /// @param pubKey The base58-encoded public key (Solana address)
    /// @param msg The message that was signed
    /// @param signature The signature to verify
    /// @return isValid True if the signature is valid
    function verifyEd25519(bytes calldata pubKey, bytes calldata msg, bytes calldata signature) external view returns (bool);
}
