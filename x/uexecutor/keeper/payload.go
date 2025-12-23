package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	utxverifiertypes "github.com/pushchain/push-chain-node/x/utxverifier/types"
)

var UNIVERSAL_PAYLOAD_TYPEHASH = common.HexToHash("0x1d8b43e5066bd20bfdacf7b8f4790c0309403b18434e3699ce3c5e57502ed8c4")

// GetPayloadHashEVM computes the EIP-712 style payload hash using the UEA contract's domainSeparator
func (k Keeper) GetPayloadHashEVM(
	ctx sdk.Context,
	from, ueaAddr common.Address,
	payload types.AbiUniversalPayload,
) (common.Hash, error) {
	// Fetch the domainSeparator from the UEA contract
	domainSep, err := k.CallUEADomainSeparator(ctx, from, ueaAddr)
	if err != nil {
		return common.Hash{}, err
	}

	// Hash payload.data
	dataHash := crypto.Keccak256Hash(payload.Data)

	// ABI encode the struct like Solidity abi.encode(...)
	arguments := AbiArgumentsForPayload()

	packed, err := arguments.Pack(
		UNIVERSAL_PAYLOAD_TYPEHASH,
		payload.To,
		payload.Value,
		dataHash,
		payload.GasLimit,
		payload.MaxFeePerGas,
		payload.MaxPriorityFeePerGas,
		payload.Nonce,
		payload.Deadline,
		payload.VType,
	)
	if err != nil {
		return common.Hash{}, err
	}

	// structHash = keccak256(encoded struct)
	structHash := crypto.Keccak256Hash(packed)

	// Final payload hash = keccak256("\x19\x01" || domainSeparator || structHash)
	finalBytes := append([]byte{0x19, 0x01}, append(domainSep[:], structHash.Bytes()...)...)
	return crypto.Keccak256Hash(finalBytes), nil
}

// StoreVerifiedPayloadHash stores one payload hash for a given inbound tx
func (k Keeper) StoreVerifiedPayloadHash(
	ctx sdk.Context,
	utx types.UniversalTx, // struct containing InboundTx
	ueaAddr common.Address,
	ueModuleAccAddress common.Address,
) error {
	// Convert to AbiUniversalPayload
	abiPayload, err := types.NewAbiUniversalPayload(utx.InboundTx.UniversalPayload)
	if err != nil {
		return fmt.Errorf("failed to convert UniversalPayload: %w", err)
	}

	// Compute payload hash
	payloadHash, err := k.GetPayloadHashEVM(ctx, ueModuleAccAddress, ueaAddr, abiPayload)
	if err != nil {
		return fmt.Errorf("failed to compute payload hash: %w", err)
	}

	// Construct VerifiedTxMetadata
	verified := utxverifiertypes.VerifiedTxMetadata{
		Minted: false,
		PayloadHashes: []string{
			payloadHash.Hex(), // store as string
		},
		UsdValue: &utxverifiertypes.USDValue{
			Amount:   "0",
			Decimals: 0,
		},
		Sender: utx.InboundTx.Sender,
	}

	// Store in utxverifierKeeper
	err = k.utxverifierKeeper.StoreVerifiedInboundTx(ctx, utx.InboundTx.SourceChain, utx.InboundTx.TxHash, verified)
	if err != nil {
		return fmt.Errorf("failed to store verified tx: %w", err)
	}

	return nil
}

// helper: parse ABI type strings safely
func mustType(t string) abi.Type {
	ty, err := abi.NewType(t, "", nil)
	if err != nil {
		panic(err)
	}
	return ty
}

// AbiArgumentsForPayload returns the abi.Arguments for encoding a UniversalPayload struct
func AbiArgumentsForPayload() abi.Arguments {
	return abi.Arguments{
		{Type: mustType("bytes32")}, // typehash
		{Type: mustType("address")},
		{Type: mustType("uint256")},
		{Type: mustType("bytes32")}, // keccak256(data)
		{Type: mustType("uint256")}, // gasLimit
		{Type: mustType("uint256")}, // maxFeePerGas
		{Type: mustType("uint256")}, // maxPriorityFeePerGas
		{Type: mustType("uint256")}, // nonce
		{Type: mustType("uint256")}, // deadline
		{Type: mustType("uint8")},   // vType
	}
}

func (k Keeper) StoreVerifiedPayloadHashForExecutePayload(
	ctx sdk.Context,
	abiPayload types.AbiUniversalPayload,
	ueaAddr common.Address,
	ueModuleAccAddress common.Address,
	sender string,
	sourceChain string,
	txHash string,
) error {
	// Compute payload hash
	payloadHash, err := k.GetPayloadHashEVM(ctx, ueModuleAccAddress, ueaAddr, abiPayload)
	if err != nil {
		return fmt.Errorf("failed to compute payload hash: %w", err)
	}

	// Construct VerifiedTxMetadata
	verified := utxverifiertypes.VerifiedTxMetadata{
		Minted: false,
		PayloadHashes: []string{
			payloadHash.Hex(), // store as string
		},
		UsdValue: &utxverifiertypes.USDValue{
			Amount:   "0",
			Decimals: 0,
		},
		Sender: sender,
	}

	// Store in utxverifierKeeper
	err = k.utxverifierKeeper.StoreVerifiedInboundTx(ctx, sourceChain, txHash, verified)
	if err != nil {
		return fmt.Errorf("failed to store verified tx: %w", err)
	}

	return nil
}
