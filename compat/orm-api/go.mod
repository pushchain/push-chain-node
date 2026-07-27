// Compatibility shim: cosmossdk.io/api dropped the cosmos/orm/* API packages
// in api v0.9.0 (and still absent in v1.0.0), but cosmossdk.io/orm (and the ORM-backed x/uregistry and
// x/uvalidator modules) still depend on them. cosmos-sdk v0.54.x requires
// api v1.0.0, so we re-provide the (unchanged) generated orm API packages here
// as a nested module and wire it in via a replace directive in the root go.mod.
// Sources copied verbatim from cosmossdk.io/api v0.8.2 (the last release that
// shipped these packages).
module cosmossdk.io/api/cosmos/orm

go 1.24.0

require (
	cosmossdk.io/api v1.0.0
	github.com/cosmos/cosmos-proto v1.0.0-beta.5
	google.golang.org/protobuf v1.36.11
)

require (
	golang.org/x/net v0.46.1-0.20251013234738-63d1a5100f82 // indirect
	golang.org/x/sys v0.37.0 // indirect
	golang.org/x/text v0.30.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260114163908-3f89685c29c3 // indirect
	google.golang.org/grpc v1.77.0 // indirect
)
