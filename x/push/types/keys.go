package types

const (
	// ModuleName defines the module name
	ModuleName = "push"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_push"
)

var (
	ParamsKey = []byte("p_push")
)

func KeyPrefix(p string) []byte {
	return []byte(p)
}
