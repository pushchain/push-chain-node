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

	// AdminParamsKey saves the current module admin params.
	AdminParamsKey = collections.NewPrefix(1)

	// AdminParamsName is the name of the admin params collection.
	AdminParamsName = "admin_params"

	// ChainConfigsKey saves the current module chainConfigs collection prefix
	ChainConfigsKey = collections.NewPrefix(2)

	// ChainConfigsName is the name of the chainConfigs collection.
	ChainConfigsName = "chain_configs"
)

const (
	ModuleName = "ue"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)

var ORMModuleSchema = ormv1alpha1.ModuleSchemaDescriptor{
	SchemaFile: []*ormv1alpha1.ModuleSchemaDescriptor_FileEntry{
		{Id: 1, ProtoFileName: "ue/v1/state.proto"},
	},
	Prefix: []byte{0},
}
