// Compatibility shim: cosmossdk.io/api dropped the cosmos/orm/* API packages
// in api v0.9.0, but cosmossdk.io/orm (and the ORM-backed x/uregistry and
// x/uvalidator modules) still depend on them. cosmos-sdk v0.53.x requires
// api v0.9.2, so we re-provide the (unchanged) generated orm API packages here
// as a nested module and wire it in via a replace directive in the root go.mod.
// Sources copied verbatim from cosmossdk.io/api v0.8.2 (the last release that
// shipped these packages).
module cosmossdk.io/api/cosmos/orm

go 1.23

require (
	cosmossdk.io/api v0.9.2
	github.com/cosmos/cosmos-proto v1.0.0-beta.5
	google.golang.org/protobuf v1.36.10
)
