// SPDX-License-Identifier: MIT
pragma solidity >=0.8.18;

/// @dev The USV contract's address.
address constant USV_PRECOMPILE_ADDRESS = 0x00000000000000000000000000000000000000ca;

/// @dev The IUSV contract's instance.
IUSV constant USV_CONTRACT = IUSV(USV_PRECOMPILE_ADDRESS);

/// @dev The IUSV contract's interface.
interface IUSV {
    /// @notice Verifies a signature using Ed25519
    /// @param pubKey The base58-encoded public key (Solana address)
    /// @param msg The message that was signed
    /// @param signature The signature to verify
    /// @return isValid True if the signature is valid
    function verifyEd25519(bytes calldata pubKey, bytes32 msg, bytes calldata signature) external view returns (bool);
}
