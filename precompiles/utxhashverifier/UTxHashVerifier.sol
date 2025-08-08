// SPDX-License-Identifier: MIT
pragma solidity >=0.8.18;

/// @dev The UTxHashVerifier contract's address.
address constant UTxHashVerifier_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000000901;

/// @dev The IUTxHashVerifier contract's instance.
IUTxHashVerifier constant UTxHashVerifier_CONTRACT = IUTxHashVerifier(UTxHashVerifier_PRECOMPILE_ADDRESS);

/// @dev The IUTxHashVerifier contract's interface.
interface IUTxHashVerifier {
    /// @notice Verifies whether a transaction hash is valid using a UniversalPayload scheme.
    /// @dev This function compares the transaction hash against the passed payload hash and verifies the owner on the specified chain.
    /// @param chainNamespace The namespace of the chain (e.g., "eip155", "solana") where the transaction was executed.
    /// @param chainId The chainId of the chain where the transaction was executed.
    /// @param owner The bytes owner key that is expected to have signed or authorized the transaction.
    /// @param payloadHash The hash of the expected universal payload parameters.
    /// @param txHash The transaction hash to be verified.
    /// @return isValid A boolean indicating whether the transaction hash is valid and correctly signed by the owner.
    function verifyTxHash(
        string calldata chainNamespace,
        string calldata chainId,
        bytes calldata owner,
        bytes32 payloadHash,
        bytes calldata txHash
    ) external returns (bool);
}
