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

// MyValidatorInfo contains status of the current node's validator
type MyValidatorInfo struct {
	IsValidator bool
	Address     string
	Moniker     string
	Status      string
	VotingPower int64
	VotingPct   float64 // Percentage of total voting power [0,1]
	Commission  string
	Jailed      bool
}
