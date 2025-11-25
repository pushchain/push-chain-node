package validator

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pushchain/push-chain-node/push-validator-manager/internal/config"
)



// rewardsCacheEntry holds cached rewards data with timestamp
type rewardsCacheEntry struct {
	commission  string
	outstanding string
	fetchedAt   time.Time
}

// Fetcher handles validator data fetching with caching
type Fetcher struct {
	mu sync.Mutex

	// All validators cache
	allValidators     ValidatorList
	allValidatorsTime time.Time

	// My validator cache
	myValidator     MyValidatorInfo
	myValidatorTime time.Time

	// Rewards cache (per validator address)
	rewardsCache map[string]rewardsCacheEntry
	rewardsTTL   time.Duration

	cacheTTL time.Duration
}

// NewFetcher creates a new validator fetcher with 30s cache
func NewFetcher() *Fetcher {
	return &Fetcher{
		cacheTTL:     30 * time.Second,
		rewardsTTL:   30 * time.Second,
		rewardsCache: make(map[string]rewardsCacheEntry),
	}
}

// GetAllValidators fetches all validators with 30s caching
func (f *Fetcher) GetAllValidators(ctx context.Context, cfg config.Config) (ValidatorList, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Force fetch on first call (cache is zero-initialized)
	if f.allValidatorsTime.IsZero() {
		list, err := f.fetchAllValidators(ctx, cfg)
		if err != nil {
			return ValidatorList{}, err
		}
		f.allValidators = list
		f.allValidatorsTime = time.Now()
		return list, nil
	}

	// Return cached if still valid
	if time.Since(f.allValidatorsTime) < f.cacheTTL && f.allValidators.Total > 0 {
		return f.allValidators, nil
	}

	// Fetch fresh data
	list, err := f.fetchAllValidators(ctx, cfg)
	if err != nil {
		// Return stale cache if available
		if f.allValidators.Total > 0 {
			return f.allValidators, nil
		}
		return ValidatorList{}, err
	}

	// Update cache
	f.allValidators = list
	f.allValidatorsTime = time.Now()
	return list, nil
}

// GetMyValidator fetches current node's validator status with 30s caching
func (f *Fetcher) GetMyValidator(ctx context.Context, cfg config.Config) (MyValidatorInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Force fetch on first call (cache is zero-initialized)
	if f.myValidatorTime.IsZero() {
		myVal, err := f.fetchMyValidator(ctx, cfg)
		if err != nil {
			// IMPORTANT: Set cache time even on error to prevent infinite retry loops
			f.myValidatorTime = time.Now()
			return MyValidatorInfo{IsValidator: false}, err
		}
		f.myValidator = myVal
		f.myValidatorTime = time.Now()
		return myVal, nil
	}

	// Return cached if still valid
	if time.Since(f.myValidatorTime) < f.cacheTTL {
		return f.myValidator, nil
	}

	// Fetch fresh data
	myVal, err := f.fetchMyValidator(ctx, cfg)
	if err != nil {
		// Return stale cache if available
		if f.myValidator.Address != "" || !f.myValidatorTime.IsZero() {
			return f.myValidator, nil
		}
		// Set cache time to retry on next refresh
		f.myValidatorTime = time.Now()
		return MyValidatorInfo{IsValidator: false}, err
	}

	// Update cache
	f.myValidator = myVal
	f.myValidatorTime = time.Now()
	return myVal, nil
}

// fetchAllValidators queries all validators from the network
func (f *Fetcher) fetchAllValidators(ctx context.Context, cfg config.Config) (ValidatorList, error) {
	bin, err := exec.LookPath("pchaind")
	if err != nil {
		return ValidatorList{}, fmt.Errorf("pchaind not found: %w", err)
	}

	remote := fmt.Sprintf("https://%s", cfg.GenesisDomain)
	cmd := exec.CommandContext(ctx, bin, "query", "staking", "validators", "--node", remote, "-o", "json")
	output, err := cmd.Output()
	if err != nil {
		return ValidatorList{}, fmt.Errorf("query validators failed: %w", err)
	}

	var result struct {
		Validators []struct {
			Description struct {
				Moniker string `json:"moniker"`
			} `json:"description"`
			OperatorAddress string `json:"operator_address"`
			Status          string `json:"status"`
			Tokens          string `json:"tokens"`
			Commission      struct {
				CommissionRates struct {
					Rate string `json:"rate"`
				} `json:"commission_rates"`
			} `json:"commission"`
			Jailed bool `json:"jailed"`
		} `json:"validators"`
	}

	if err := json.Unmarshal(output, &result); err != nil {
		return ValidatorList{}, fmt.Errorf("parse validators failed: %w", err)
	}

	validators := make([]ValidatorInfo, 0, len(result.Validators))
	for _, v := range result.Validators {
		moniker := v.Description.Moniker
		if moniker == "" {
			moniker = "unknown"
		}

		status := parseStatus(v.Status)

		var votingPower int64
		if v.Tokens != "" {
			if tokens, err := strconv.ParseFloat(v.Tokens, 64); err == nil {
				votingPower = int64(tokens / 1e18)
			}
		}

		commission := "0%"
		if v.Commission.CommissionRates.Rate != "" {
			if rate, err := strconv.ParseFloat(v.Commission.CommissionRates.Rate, 64); err == nil {
				commission = fmt.Sprintf("%.0f%%", rate*100)
			}
		}

		validators = append(validators, ValidatorInfo{
			OperatorAddress: v.OperatorAddress,
			Moniker:         moniker,
			Status:          status,
			Tokens:          v.Tokens,
			VotingPower:     votingPower,
			Commission:      commission,
			Jailed:          v.Jailed,
		})
	}

	return ValidatorList{
		Validators: validators,
		Total:      len(validators),
	}, nil
}

// fetchMyValidator fetches the current node's validator info by comparing consensus pubkeys
func (f *Fetcher) fetchMyValidator(ctx context.Context, cfg config.Config) (MyValidatorInfo, error) {
	bin, err := exec.LookPath("pchaind")
	if err != nil {
		return MyValidatorInfo{IsValidator: false}, nil
	}

	// Get local consensus pubkey using 'tendermint show-validator'
	showValCmd := exec.CommandContext(ctx, bin, "tendermint", "show-validator", "--home", cfg.HomeDir)
	pubkeyBytes, err := showValCmd.Output()
	if err != nil {
		// No validator key file exists
		return MyValidatorInfo{IsValidator: false}, nil
	}

	var localPubkey struct {
		Type string `json:"@type"`
		Key  string `json:"key"`
	}
	if err := json.Unmarshal(pubkeyBytes, &localPubkey); err != nil {
		return MyValidatorInfo{IsValidator: false}, nil
	}

	if localPubkey.Key == "" {
		return MyValidatorInfo{IsValidator: false}, nil
	}

	// Build the full pubkey JSON string for slashing info query
	fullPubkeyJSON := string(pubkeyBytes)

	// Get local node moniker from status (for conflict detection)
	var localMoniker string
	statusCmd := exec.CommandContext(ctx, bin, "status", "--node", cfg.RPCLocal)
	if statusOutput, err := statusCmd.Output(); err == nil {
		var statusData struct {
			NodeInfo struct {
				Moniker string `json:"moniker"`
			} `json:"NodeInfo"`
		}
		if json.Unmarshal(statusOutput, &statusData) == nil {
			localMoniker = statusData.NodeInfo.Moniker
		}
	}

	// Fetch all validators to match by consensus pubkey
	remote := fmt.Sprintf("https://%s", cfg.GenesisDomain)
	queryCmd := exec.CommandContext(ctx, bin, "query", "staking", "validators", "--node", remote, "-o", "json")
	valsOutput, err := queryCmd.Output()
	if err != nil {
		return MyValidatorInfo{IsValidator: false}, err
	}

	var result struct {
		Validators []struct {
			OperatorAddress string `json:"operator_address"`
			Description     struct {
				Moniker string `json:"moniker"`
			} `json:"description"`
			ConsensusPubkey struct {
				Value string `json:"value"` // The base64 pubkey
			} `json:"consensus_pubkey"`
			Status     string `json:"status"`
			Tokens     string `json:"tokens"`
			Commission struct {
				CommissionRates struct {
					Rate string `json:"rate"`
				} `json:"commission_rates"`
			} `json:"commission"`
			Jailed bool `json:"jailed"`
		} `json:"validators"`
	}

	if err := json.Unmarshal(valsOutput, &result); err != nil {
		return MyValidatorInfo{IsValidator: false}, err
	}

	// Calculate total voting power
	var totalVotingPower int64
	for _, v := range result.Validators {
		if v.Tokens != "" {
			if tokens, err := strconv.ParseFloat(v.Tokens, 64); err == nil {
				totalVotingPower += int64(tokens / 1e18)
			}
		}
	}

	// Try to find validator by matching consensus pubkey
	var monikerConflict string
	for _, v := range result.Validators {
		// Check for moniker conflicts (different validator, same moniker)
		if localMoniker != "" && v.Description.Moniker == localMoniker &&
		   !strings.EqualFold(v.ConsensusPubkey.Value, localPubkey.Key) {
			monikerConflict = localMoniker
		}

		// Check if this validator matches our consensus pubkey
		if strings.EqualFold(v.ConsensusPubkey.Value, localPubkey.Key) {
			// Found our validator!
			status := parseStatus(v.Status)

			var votingPower int64
			if v.Tokens != "" {
				if tokens, err := strconv.ParseFloat(v.Tokens, 64); err == nil {
					votingPower = int64(tokens / 1e18)
				}
			}

			var votingPct float64
			if totalVotingPower > 0 {
				votingPct = float64(votingPower) / float64(totalVotingPower)
			}

			commission := "0%"
			if v.Commission.CommissionRates.Rate != "" {
				if rate, err := strconv.ParseFloat(v.Commission.CommissionRates.Rate, 64); err == nil {
					commission = fmt.Sprintf("%.0f%%", rate*100)
				}
			}

			info := MyValidatorInfo{
				IsValidator:                  true,
				Address:                      v.OperatorAddress,
				Moniker:                      v.Description.Moniker,
				Status:                       status,
				VotingPower:                  votingPower,
				VotingPct:                    votingPct,
				Commission:                   commission,
				Jailed:                       v.Jailed,
				ValidatorExistsWithSameMoniker: monikerConflict != "",
				ConflictingMoniker:            monikerConflict,
			}

			// If jailed, fetch slashing info with timeout (10s)
			if v.Jailed {
				slashCtx, slashCancel := context.WithTimeout(context.Background(), 10*time.Second)
				slashingInfo, err := GetSlashingInfo(slashCtx, cfg, fullPubkeyJSON)
				slashCancel()
				if err == nil {
					info.SlashingInfo = slashingInfo
				} else {
					// Store error for display in UI
					info.SlashingInfoError = fmt.Sprintf("Failed to fetch jail reason: %v", err)
				}
			}

			return info, nil
		}
	}

	// Not matched by consensus pubkey, check for keyring address match
	// (validator may have been created with a key in the local keyring)
	keyringAddrs := getKeyringAddresses(bin, cfg)
	for _, keyAddr := range keyringAddrs {
		for _, v := range result.Validators {
			// Check if validator's operator address matches a key in the keyring
			// Convert cosmos address to validator operator address for comparison
			// Both addresses have the same bech32 data, just different prefixes
			cosmosPrefix := "push1"
			validatorPrefix := "pushvaloper1"

			// Extract the bech32-encoded part (remove prefix)
			keyAddrData := strings.TrimPrefix(keyAddr, cosmosPrefix)
			valAddrData := strings.TrimPrefix(v.OperatorAddress, validatorPrefix)

			if keyAddrData != "" && keyAddrData == valAddrData {
				// Found validator controlled by a key in our keyring
				status := parseStatus(v.Status)

				var votingPower int64
				if v.Tokens != "" {
					if tokens, err := strconv.ParseFloat(v.Tokens, 64); err == nil {
						votingPower = int64(tokens / 1e18)
					}
				}

				var votingPct float64
				if totalVotingPower > 0 {
					votingPct = float64(votingPower) / float64(totalVotingPower)
				}

				commission := "0%"
				if v.Commission.CommissionRates.Rate != "" {
					if rate, err := strconv.ParseFloat(v.Commission.CommissionRates.Rate, 64); err == nil {
						commission = fmt.Sprintf("%.0f%%", rate*100)
					}
				}

				// Return validator info but with IsValidator=false (no consensus pubkey match)
				// This indicates keyring match but consensus key mismatch
				return MyValidatorInfo{
					IsValidator:                    false,
					Address:                        v.OperatorAddress,
					Moniker:                        v.Description.Moniker,
					Status:                         status,
					VotingPower:                    votingPower,
					VotingPct:                      votingPct,
					Commission:                     commission,
					Jailed:                         v.Jailed,
					ValidatorExistsWithSameMoniker: false,
					ConflictingMoniker:            "",
				}, nil
			}
		}
	}

	// Not matched by consensus pubkey, check for moniker-based match
	// (validator may have been created with different key/node)
	if localMoniker != "" {
		for _, v := range result.Validators {
			if v.Description.Moniker == localMoniker {
				// Found validator by moniker but consensus pubkey doesn't match
				status := parseStatus(v.Status)

				var votingPower int64
				if v.Tokens != "" {
					if tokens, err := strconv.ParseFloat(v.Tokens, 64); err == nil {
						votingPower = int64(tokens / 1e18)
					}
				}

				var votingPct float64
				if totalVotingPower > 0 {
					votingPct = float64(votingPower) / float64(totalVotingPower)
				}

				commission := "0%"
				if v.Commission.CommissionRates.Rate != "" {
					if rate, err := strconv.ParseFloat(v.Commission.CommissionRates.Rate, 64); err == nil {
						commission = fmt.Sprintf("%.0f%%", rate*100)
					}
				}

				// Return validator info but with IsValidator=false (no consensus pubkey match)
				// This indicates moniker match but key/node mismatch
				return MyValidatorInfo{
					IsValidator:                    false,
					Address:                        v.OperatorAddress,
					Moniker:                        v.Description.Moniker,
					Status:                         status,
					VotingPower:                    votingPower,
					VotingPct:                      votingPct,
					Commission:                     commission,
					Jailed:                         v.Jailed,
					ValidatorExistsWithSameMoniker: false,
					ConflictingMoniker:            "",
				}, nil
			}
		}
	}

	// Not registered as validator, but check for moniker conflicts
	return MyValidatorInfo{
		IsValidator:                  false,
		ValidatorExistsWithSameMoniker: monikerConflict != "",
		ConflictingMoniker:            monikerConflict,
	}, nil
}

// parseStatus converts bond status to human-readable format
func parseStatus(status string) string {
	switch status {
	case "BOND_STATUS_BONDED":
		return "BONDED"
	case "BOND_STATUS_UNBONDING":
		return "UNBONDING"
	case "BOND_STATUS_UNBONDED":
		return "UNBONDED"
	default:
		return status
	}
}

// GetValidatorRewards fetches commission and outstanding rewards for a validator
func GetValidatorRewards(ctx context.Context, cfg config.Config, validatorAddr string) (commission string, outstanding string, err error) {
	if validatorAddr == "" {
		return "—", "—", fmt.Errorf("validator address required")
	}

	bin, err := exec.LookPath("pchaind")
	if err != nil {
		return "—", "—", fmt.Errorf("pchaind not found: %w", err)
	}

	// Create child context with 15s timeout per validator to avoid deadline issues
	// when fetching rewards for multiple validators in parallel
	// Increased from 5s to handle network latency and slower nodes
	queryCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	remote := fmt.Sprintf("https://%s", cfg.GenesisDomain)

	// Fetch commission rewards
	commissionRewards := "—"
	commCmd := exec.CommandContext(queryCtx, bin, "query", "distribution", "commission", validatorAddr, "--node", remote, "-o", "json")
	if commOutput, err := commCmd.Output(); err == nil {
		var commResult struct {
			Commission struct {
				Commission []string `json:"commission"`
			} `json:"commission"`
		}
		if err := json.Unmarshal(commOutput, &commResult); err == nil && len(commResult.Commission.Commission) > 0 {
			// Extract numeric part from amount string (format: "123.45upc")
			amountStr := commResult.Commission.Commission[0]
			// Remove denom suffix
			amountStr = strings.TrimSuffix(amountStr, "upc")
			if amount, err := strconv.ParseFloat(amountStr, 64); err == nil {
				commissionRewards = fmt.Sprintf("%.2f", amount/1e18)
			}
		}
	}

	// Fetch outstanding rewards with retry logic
	outstandingRewards := "—"
	var outOutput []byte
	var outErr error

	// Retry up to 2 times with 2s delay on failure
	maxRetries := 2
	for attempt := 0; attempt <= maxRetries; attempt++ {
		outCmd := exec.CommandContext(queryCtx, bin, "query", "distribution", "validator-outstanding-rewards", validatorAddr, "--node", remote, "-o", "json")
		outOutput, outErr = outCmd.Output()

		if outErr == nil {
			break // Success, exit retry loop
		}

		// Wait before retry (except on last attempt)
		if attempt < maxRetries {
			time.Sleep(2 * time.Second)
		}
	}

	// Parse outstanding rewards if fetch succeeded
	if outErr == nil {
		var outResult struct {
			Rewards struct {
				Rewards []string `json:"rewards"`
			} `json:"rewards"`
		}
		if err := json.Unmarshal(outOutput, &outResult); err == nil && len(outResult.Rewards.Rewards) > 0 {
			// Extract numeric part from amount string (format: "123.45upc")
			amountStr := outResult.Rewards.Rewards[0]
			// Remove denom suffix
			amountStr = strings.TrimSuffix(amountStr, "upc")
			if amount, err := strconv.ParseFloat(amountStr, 64); err == nil {
				outstandingRewards = fmt.Sprintf("%.2f", amount/1e18)
			}
		}
	}

	return commissionRewards, outstandingRewards, nil
}

// GetCachedValidatorRewards fetches validator rewards with 30s caching
func (f *Fetcher) GetCachedValidatorRewards(ctx context.Context, cfg config.Config, validatorAddr string) (commission string, outstanding string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Check cache first
	if cached, exists := f.rewardsCache[validatorAddr]; exists {
		if time.Since(cached.fetchedAt) < f.rewardsTTL {
			return cached.commission, cached.outstanding, nil
		}
	}

	// Cache miss or expired - fetch fresh data
	commission, outstanding, err = GetValidatorRewards(ctx, cfg, validatorAddr)
	if err == nil {
		// Store in cache
		f.rewardsCache[validatorAddr] = rewardsCacheEntry{
			commission:  commission,
			outstanding: outstanding,
			fetchedAt:   time.Now(),
		}
	}

	return commission, outstanding, err
}

// Global fetcher instance
var globalFetcher = NewFetcher()

// GetCachedValidatorsList returns cached validator list
func GetCachedValidatorsList(ctx context.Context, cfg config.Config) (ValidatorList, error) {
	return globalFetcher.GetAllValidators(ctx, cfg)
}

// GetCachedMyValidator returns cached my validator info
func GetCachedMyValidator(ctx context.Context, cfg config.Config) (MyValidatorInfo, error) {
	return globalFetcher.GetMyValidator(ctx, cfg)
}

// GetCachedRewards returns validator rewards with 30s caching
func GetCachedRewards(ctx context.Context, cfg config.Config, validatorAddr string) (commission string, outstanding string, err error) {
	return globalFetcher.GetCachedValidatorRewards(ctx, cfg, validatorAddr)
}

// GetEVMAddress converts a Cosmos validator address to EVM address
func GetEVMAddress(ctx context.Context, validatorAddr string) string {
	if validatorAddr == "" {
		return "—"
	}

	bin, err := exec.LookPath("pchaind")
	if err != nil {
		return "—"
	}

	cmd := exec.CommandContext(ctx, bin, "debug", "addr", validatorAddr)
	output, err := cmd.Output()
	if err != nil {
		return "—"
	}

	// Parse output to extract hex address
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Address (hex):") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				hex := strings.TrimSpace(parts[1])
				return "0x" + hex
			}
		}
	}

	return "—"
}

// GetSlashingInfo fetches slashing information for a validator (jail reason, jailed until time, etc)
func GetSlashingInfo(ctx context.Context, cfg config.Config, consensusPubkey string) (SlashingInfo, error) {
	bin, err := exec.LookPath("pchaind")
	if err != nil {
		return SlashingInfo{}, fmt.Errorf("pchaind not found: %w", err)
	}

	remote := fmt.Sprintf("https://%s", cfg.GenesisDomain)

	// Query signing info to get jail details
	// consensusPubkey should be a JSON string like: {"@type":"/cosmos.crypto.ed25519.PubKey","key":"..."}
	cmd := exec.CommandContext(ctx, bin, "query", "slashing", "signing-info", consensusPubkey, "--node", remote, "-o", "json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return SlashingInfo{}, fmt.Errorf("failed to query slashing info: %w", err)
	}

	var result struct {
		ValSigningInfo struct {
			Address      string `json:"address"`
			StartHeight  string `json:"start_height"`
			JailedUntil  string `json:"jailed_until"`
			Tombstoned   bool   `json:"tombstoned"`
			MissedBlocks string `json:"missed_blocks_counter"`
		} `json:"val_signing_info"`
	}

	if err := json.Unmarshal(output, &result); err != nil {
		return SlashingInfo{}, fmt.Errorf("failed to parse slashing info: %w", err)
	}

	info := SlashingInfo{
		Tombstoned:  result.ValSigningInfo.Tombstoned,
		JailedUntil: result.ValSigningInfo.JailedUntil,
	}

	// Parse missed blocks counter
	if result.ValSigningInfo.MissedBlocks != "" {
		if mb, err := strconv.ParseInt(result.ValSigningInfo.MissedBlocks, 10, 64); err == nil {
			info.MissedBlocks = mb
		}
	}

	// Determine jail reason with better heuristics
	if info.Tombstoned {
		info.JailReason = "Double Sign"
	} else if info.JailedUntil != "" && info.JailedUntil != "1970-01-01T00:00:00Z" {
		// Valid jail time (not epoch) indicates downtime
		info.JailReason = "Downtime"
	} else if info.MissedBlocks > 0 {
		// If missed blocks > 0, it's likely a downtime issue
		info.JailReason = "Downtime"
	} else {
		// Unable to determine reason
		info.JailReason = "Unknown"
	}

	return info, nil
}

// getKeyringAddresses returns all addresses in the local keyring
func getKeyringAddresses(bin string, cfg config.Config) []string {
	var addresses []string

	// List all keys in the keyring
	cmd := exec.Command(bin, "keys", "list", "--keyring-backend", cfg.KeyringBackend, "--home", cfg.HomeDir, "-o", "json")
	output, err := cmd.Output()
	if err != nil {
		return addresses
	}

	// Parse the JSON output to extract addresses
	var keys []struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal(output, &keys); err != nil {
		return addresses
	}

	for _, key := range keys {
		if key.Address != "" {
			addresses = append(addresses, key.Address)
		}
	}

	return addresses
}
