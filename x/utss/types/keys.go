package types

import (
	"cosmossdk.io/collections"
)

var (
	// ParamsKey saves the current module params.
	ParamsKey = collections.NewPrefix(0)

	// ParamsName is the name of the params collection.
	ParamsName = "params"

	// NextProcessIdKey saves the current module NextProcessId.
	NextProcessIdKey = collections.NewPrefix(1)

	// NextProcessIdName is the name of the NextProcessId collection.
	NextProcessIdName = "next_process_id"

	// CurrentTssProcessKey saves the current module CurrentTssProcess.
	CurrentTssProcessKey = collections.NewPrefix(2)

	// CurrentTssProcessName is the name of the CurrentTssProcess collection.
	CurrentTssProcessName = "current_tss_process"

	// ProcessHistoryKey saves the current module ProcessHistory.
	ProcessHistoryKey = collections.NewPrefix(3)

	// ProcessHistoryName is the name of the ProcessHistory collection.
	ProcessHistoryName = "process_history"

	// CurrentTssKeyPrefix saves the current module CurrentTssKey.
	CurrentTssKeyKeyPrefix = collections.NewPrefix(4)

	// CurrentTssKeyName is the name of the CurrentTssKey collection.
	CurrentTssKeyName = "current_tss_key"

	// TssKeyHistoryKey saves the current module TssKeyHistory.
	TssKeyHistoryKey = collections.NewPrefix(5)

	// TssKeyHistoryName is the name of the TssKeyHistory collection.
	TssKeyHistoryName = "tss_key_history"
)

const (
	ModuleName = "utss"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)
