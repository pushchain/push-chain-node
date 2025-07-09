// SPDX-License-Identifier: MIT
pragma solidity >=0.8.18;

/// @dev The OCV contract's address.
address constant OCV_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000000901;

/// @dev The IOCV contract's instance.
IOCV constant OCV_CONTRACT = IOCV(OCV_PRECOMPILE_ADDRESS);

/// @dev UniversalPayload struct for verification
struct UniversalPayload {
    address to; // Target contract address to call
    uint256 value; // Native token amount to send
    bytes data; // Call data for the function execution
    uint256 gasLimit; // Maximum gas to be used for this tx
    uint256 maxFeePerGas; // Maximum fee per gas unit
    uint256 maxPriorityFeePerGas; // Maximum priority fee per gas unit
    uint256 nonce; // Chain ID where this should be executed
    uint256 deadline; // Timestamp after which this payload is invalid
    uint8 vType; // Type of verification to use (0=signedVerification, 1=universalTxVerification)
}

/// @dev The IOCV contract's interface.
interface IOCV {
    /// @notice Verifies transaction hash using UniversalPayload
    /// @param payload The universal payload containing verification parameters
    /// @param txHash The transaction hash as bytes
    /// @param ownerKey The owner key to verify against the transaction
    /// @param chain The chain ID where this should be executed
    /// @return isValid True if the verification is successful
    function verifyTxHash(
        UniversalPayload calldata payload,
        bytes calldata txHash,
        string calldata ownerKey,
        string calldata chain
    ) external returns (bool);
}
