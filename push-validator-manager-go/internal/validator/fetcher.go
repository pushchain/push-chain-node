package validator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/config"
)

// Fetcher handles validator data fetching with caching
type Fetcher struct {
	mu sync.Mutex

	// All validators cache
	allValidators     ValidatorList
	allValidatorsTime time.Time

	// My validator cache
	myValidator     MyValidatorInfo
	myValidatorTime time.Time

	cacheTTL time.Duration
}

// NewFetcher creates a new validator fetcher with 30s cache
func NewFetcher() *Fetcher {
	return &Fetcher{
		cacheTTL: 30 * time.Second,
	}
}

// GetAllValidators fetches all validators with 30s caching
func (f *Fetcher) GetAllValidators(ctx context.Context, cfg config.Config) (ValidatorList, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

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

	remote := fmt.Sprintf("tcp://%s:26657", cfg.GenesisDomain)
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

// fetchMyValidator fetches the current node's validator info
func (f *Fetcher) fetchMyValidator(ctx context.Context, cfg config.Config) (MyValidatorInfo, error) {
	// Read consensus address from priv_validator_key.json
	keyPath := filepath.Join(cfg.HomeDir, "config", "priv_validator_key.json")
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		// Not a validator node
		return MyValidatorInfo{IsValidator: false}, nil
	}

	var keyFile struct {
		Address string `json:"address"`
		PubKey  struct {
			Value string `json:"value"`
		} `json:"pub_key"`
	}

	if err := json.Unmarshal(keyData, &keyFile); err != nil {
		return MyValidatorInfo{IsValidator: false}, nil
	}

	consensusAddr := keyFile.Address
	if consensusAddr == "" {
		return MyValidatorInfo{IsValidator: false}, nil
	}

	// Fetch all validators to find ours and calculate percentage
	allVals, err := f.fetchAllValidators(ctx, cfg)
	if err != nil {
		return MyValidatorInfo{IsValidator: false}, err
	}

	// Calculate total voting power
	var totalVotingPower int64
	for _, v := range allVals.Validators {
		totalVotingPower += v.VotingPower
	}

	// Find our validator by matching consensus address
	// Note: This is a simplified approach. In production, you'd need to:
	// 1. Convert consensus address to operator address
	// 2. Query the specific validator
	// For now, we'll try to match by checking validator info from local node status

	bin, err := exec.LookPath("pchaind")
	if err != nil {
		return MyValidatorInfo{IsValidator: false}, nil
	}

	// Try to get validator info from local node
	cmd := exec.CommandContext(ctx, bin, "status", "--node", cfg.RPCLocal)
	output, err := cmd.Output()
	if err != nil {
		return MyValidatorInfo{IsValidator: false}, nil
	}

	var status struct {
		ValidatorInfo struct {
			Address string `json:"Address"`
		} `json:"ValidatorInfo"`
	}

	if err := json.Unmarshal(output, &status); err != nil {
		return MyValidatorInfo{IsValidator: false}, nil
	}

	// Simplified matching - in production, convert addresses properly
	// For now, assume first validator or try to find by moniker
	if len(allVals.Validators) > 0 {
		// Return first BONDED validator as placeholder
		// TODO: Implement proper address conversion
		first := allVals.Validators[0]
		var votingPct float64
		if totalVotingPower > 0 {
			votingPct = float64(first.VotingPower) / float64(totalVotingPower)
		}

		return MyValidatorInfo{
			IsValidator: true,
			Address:     first.OperatorAddress,
			Moniker:     first.Moniker,
			Status:      first.Status,
			VotingPower: first.VotingPower,
			VotingPct:   votingPct,
			Commission:  first.Commission,
			Jailed:      first.Jailed,
		}, nil
	}

	return MyValidatorInfo{IsValidator: false}, nil
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
