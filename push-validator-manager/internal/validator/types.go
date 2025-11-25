package validator

// ValidatorInfo contains information about a single validator
type ValidatorInfo struct {
	OperatorAddress string
	Moniker         string
	Status          string // BONDED, UNBONDING, UNBONDED
	Tokens          string // Raw token amount
	VotingPower     int64  // Tokens converted to power
	Commission      string // Commission rate as percentage
	Jailed          bool
}

// ValidatorList contains a list of validators
type ValidatorList struct {
	Validators []ValidatorInfo
	Total      int
}

// SlashingInfo contains slashing-related information for a validator
type SlashingInfo struct {
	Tombstoned       bool
	JailedUntil      string // RFC3339 formatted timestamp
	MissedBlocks     int64
	JailReason       string // "Downtime", "Double Sign", or "Unknown"
}

// MyValidatorInfo contains status of the current node's validator
type MyValidatorInfo struct {
	IsValidator                  bool
	Address                      string
	Moniker                      string
	Status                       string
	VotingPower                  int64
	VotingPct                    float64 // Percentage of total voting power [0,1]
	Commission                   string
	Jailed                       bool
	SlashingInfo                 SlashingInfo // Jail reason and details
	SlashingInfoError            string       // Error message if slashing info fetch failed
	ValidatorExistsWithSameMoniker bool   // True if a different validator uses this node's moniker
	ConflictingMoniker            string // The moniker that conflicts
}
