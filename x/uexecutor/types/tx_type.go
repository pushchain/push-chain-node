package types

// Solidity TX_TYPE (uint8) → Cosmos TxType
func SolidityTxTypeToProto(txTypeUint8 uint8) TxType {
	switch txTypeUint8 {
	case 0:
		return TxType_GAS
	case 1:
		return TxType_GAS_AND_PAYLOAD
	case 2:
		return TxType_FUNDS
	case 3:
		return TxType_FUNDS_AND_PAYLOAD
	case 4:
		return TxType_PAYLOAD
	case 5:
		return TxType_INBOUND_REVERT
	default:
		return TxType_UNSPECIFIED_TX
	}
}

// Cosmos TxType → Solidity uint8 (for emitting events from core module if ever needed)
func ProtoTxTypeToSolidity(txType TxType) uint8 {
	switch txType {
	case TxType_GAS:
		return 0
	case TxType_GAS_AND_PAYLOAD:
		return 1
	case TxType_FUNDS:
		return 2
	case TxType_FUNDS_AND_PAYLOAD:
		return 3
	default:
		return 0 // fallback
	}
}
