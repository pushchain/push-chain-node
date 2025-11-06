package tss

// TssProcessType represents the type of TSS process
type TssProcessType string

const (
	TssProcessTypeKeyGen     TssProcessType = "keygen"
	TssProcessTypeKeyRefresh TssProcessType = "keyrefresh"
	TssProcessTypeSign       TssProcessType = "sign"
)
