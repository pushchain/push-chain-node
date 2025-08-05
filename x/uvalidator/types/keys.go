package types

import (
	"cosmossdk.io/collections"

	ormv1alpha1 "cosmossdk.io/api/cosmos/orm/v1alpha1"
)

var (

	// ParamsKey saves the current module params.
	ParamsKey = collections.NewPrefix(0)

	// ParamsName is the name of the params collection.
	ParamsName = "params"

	// CoreToUniversalKey is the key for the mapping of core validator addresses to universal validator addresses.
	CoreToUniversalKey = collections.NewPrefix(1)

	// CoreToUniversalName is the name of the core to universal mapping.
	CoreToUniversalName = "core_to_universal"

	// CoreValidatorSetKey is the key for the set of core validator addresses.
	CoreValidatorSetKey = collections.NewPrefix(2)

	// CoreValidatorSetName is the name of the core validator set.
	CoreValidatorSetName = "core_validator_set"
)

const (
	ModuleName = "uvalidator"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)

var ORMModuleSchema = ormv1alpha1.ModuleSchemaDescriptor{
	SchemaFile: []*ormv1alpha1.ModuleSchemaDescriptor_FileEntry{
		{Id: 1, ProtoFileName: "uvalidator/v1/state.proto"},
	},
	Prefix: []byte{0},
}
