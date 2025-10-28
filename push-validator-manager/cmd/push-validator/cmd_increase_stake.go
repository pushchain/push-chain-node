package main

import (
	"bufio"
	"context"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pushchain/push-chain-node/push-validator-manager/internal/config"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/validator"
)

// handleIncreaseStake allows validators to increase their stake after registration
func handleIncreaseStake(cfg config.Config) {
	v := validator.NewWith(validator.Options{
		BinPath:       findPchaind(),
		HomeDir:       cfg.HomeDir,
		ChainID:       cfg.ChainID,
		Keyring:       cfg.KeyringBackend,
		GenesisDomain: cfg.GenesisDomain,
		Denom:         cfg.Denom,
	})

	// Get validator info
	valCtx, valCancel := context.WithTimeout(context.Background(), 20*time.Second)
	myValInfo, valErr := validator.GetCachedMyValidator(valCtx, cfg)
	valCancel()

	if valErr != nil {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": valErr.Error()})
		} else {
			fmt.Println()
			fmt.Println(getPrinter().Colors.Error("⚠️ Failed to retrieve validator information"))
			fmt.Printf("Error: %v\n\n", valErr)
			fmt.Println(getPrinter().Colors.Info("Make sure you are registered as a validator first:"))
			fmt.Println(getPrinter().Colors.Apply(getPrinter().Colors.Theme.Command, "  push-validator register-validator"))
			fmt.Println()
		}
		return
	}

	if !myValInfo.IsValidator {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": "not a registered validator"})
		} else {
			fmt.Println()
			fmt.Println(getPrinter().Colors.Error("❌ This node is not registered as a validator"))
			fmt.Println()
			fmt.Println(getPrinter().Colors.Info("To register, use:"))
			fmt.Println(getPrinter().Colors.Apply(getPrinter().Colors.Theme.Command, "  push-validator register-validator"))
			fmt.Println()
		}
		return
	}

	// Display current validator info
	p := getPrinter()
	fmt.Println()
	p.Section("Current Validator Status")
	fmt.Println()
	p.KeyValueLine("Validator Name", myValInfo.Moniker, "blue")
	p.KeyValueLine("Address", myValInfo.Address, "dim")

	// Display voting power (converted from int64 to PC)
	votingPowerPC := float64(myValInfo.VotingPower) / 1e6 // Voting power is in units of 1e-6
	p.KeyValueLine("Voting Power", fmt.Sprintf("%.6f", votingPowerPC)+" PC", "yellow")
	fmt.Println()

	// Convert validator operator address to account address
	accountAddr, convErr := convertValidatorToAccountAddress(myValInfo.Address)
	if convErr != nil {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": convErr.Error()})
		} else {
			fmt.Println(p.Colors.Error("⚠️ Failed to convert validator address"))
			fmt.Printf("Error: %v\n\n", convErr)
		}
		return
	}

	// Get account balance from Cosmos SDK
	balCtx, balCancel := context.WithTimeout(context.Background(), 15*time.Second)
	balance, balErr := v.Balance(balCtx, accountAddr)
	balCancel()

	if balErr != nil {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": balErr.Error()})
		} else {
			fmt.Println(p.Colors.Error("⚠️ Failed to retrieve balance"))
			fmt.Printf("Error: %v\n\n", balErr)
		}
		return
	}

	// Display balance info
	const minDelegate = "100000000000000000"        // 0.1 PC in wei
	const feeReserve = "100000000000000000"         // 0.1 PC in wei for gas fees

	balInt := new(big.Int)
	balInt.SetString(balance, 10)
	feeInt := new(big.Int)
	feeInt.SetString(feeReserve, 10)
	maxDelegatable := new(big.Int).Sub(balInt, feeInt)

	// Handle case where balance is less than fee
	if maxDelegatable.Sign() < 0 {
		maxDelegatable.SetInt64(0)
	}

	divisor := new(big.Float).SetFloat64(1e18)
	balFloat, _ := new(big.Float).SetString(balance)
	balPC := new(big.Float).Quo(balFloat, divisor)

	maxDelegateFloat, _ := new(big.Float).SetString(maxDelegatable.String())
	maxDelegatePC := new(big.Float).Quo(maxDelegateFloat, divisor)

	p.Section("Account Balance")
	fmt.Println()
	p.KeyValueLine("Available Balance", fmt.Sprintf("%.6f", balPC)+" PC", "blue")
	p.KeyValueLine("Available to Delegate", fmt.Sprintf("%.6f", maxDelegatePC)+" PC", "blue")
	p.KeyValueLine("Reserved for Fees", "0.1 PC", "dim")
	fmt.Println()

	// Check if user has enough balance
	if maxDelegatable.Sign() <= 0 {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": "insufficient balance"})
		} else {
			fmt.Println(p.Colors.Error("❌ Insufficient balance to delegate"))
			fmt.Println()
			fmt.Println("You need at least 0.2 PC to increase stake (0.1 PC to delegate + 0.1 PC for fees).")
			fmt.Println()
		}
		return
	}

	// Prompt for delegation amount
	reader := bufio.NewReader(os.Stdin)
	minDelegatePC := 0.1
	maxDelegatePCVal, _ := strconv.ParseFloat(fmt.Sprintf("%.6f", maxDelegatePC), 64)

	delegationAmount := ""
	for {
		fmt.Printf("Enter amount to delegate (%.1f - %.1f PC): ", minDelegatePC, maxDelegatePCVal)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" {
			fmt.Println(p.Colors.Error("⚠ Amount is required. Try again."))
			continue
		}

		// Parse user input
		delegateAmount, err := strconv.ParseFloat(input, 64)
		if err != nil {
			fmt.Println(p.Colors.Error("⚠ Invalid amount. Enter a number. Try again."))
			continue
		}

		// Validate bounds
		if delegateAmount < minDelegatePC {
			fmt.Printf(p.Colors.Error("⚠ Amount too low. Minimum delegation is %.1f PC. Try again.\n"), minDelegatePC)
			continue
		}
		if delegateAmount > maxDelegatePCVal {
			fmt.Printf(p.Colors.Error("⚠ Insufficient balance. Maximum: %.1f PC. Try again.\n"), maxDelegatePCVal)
			continue
		}

		// Convert to wei
		delegateWei := new(big.Float).Mul(new(big.Float).SetFloat64(delegateAmount), new(big.Float).SetFloat64(1e18))
		delegationAmount = delegateWei.Text('f', 0)

		fmt.Printf(p.Colors.Success("✓ Will delegate %.6f PC\n"), delegateAmount)
		fmt.Println()
		break
	}

	// Auto-derive key name from validator
	defaultKeyName := getenvDefault("KEY_NAME", "validator-key")
	var keyName string

	// Try to auto-derive the key name from the validator's address
	if myValInfo.Address != "" {
		// We already have accountAddr from the balance check above, but need to recalculate
		// in case that logic changes in the future
		accountAddr, convErr := convertValidatorToAccountAddress(myValInfo.Address)
		if convErr == nil {
			// Try to find the key in the keyring
			if foundKey, findErr := findKeyNameByAddress(cfg, accountAddr); findErr == nil {
				keyName = foundKey
				if flagOutput != "json" {
					fmt.Println()
					fmt.Printf("🔑 Using key: %s\n", keyName)
				}
			} else {
				// Fall back to default if key not found
				keyName = defaultKeyName
			}
		} else {
			// Fall back to default if address conversion failed
			keyName = defaultKeyName
		}
	} else {
		keyName = defaultKeyName
	}

	if keyName == "" {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": "could not determine key name"})
		} else {
			fmt.Println(p.Colors.Error("⚠️ Could not determine key name"))
			fmt.Println()
		}
		return
	}

	// Execute delegation
	fmt.Println(p.Colors.Info("Submitting delegation transaction..."))
	fmt.Println()

	delegCtx, delegCancel := context.WithTimeout(context.Background(), 90*time.Second)
	txHash, delegErr := v.Delegate(delegCtx, validator.DelegateArgs{
		ValidatorAddress: myValInfo.Address,
		Amount:           delegationAmount,
		KeyName:          keyName,
	})
	delegCancel()

	if delegErr != nil {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": delegErr.Error()})
		} else {
			fmt.Println()
			fmt.Println(p.Colors.Error("❌ Delegation failed"))
			fmt.Printf("Error: %v\n\n", delegErr)
		}
		return
	}

	// Success output
	if flagOutput == "json" {
		getPrinter().JSON(map[string]any{
			"ok":                true,
			"txhash":            txHash,
			"delegation_amount": delegationAmount,
		})
	} else {
		fmt.Println()
		p.Success("✅ Delegation successful!")
		fmt.Println()

		// Display delegation details
		p.KeyValueLine("Transaction Hash", txHash, "green")

		// Display delegation amount
		delegateFloat, _ := new(big.Float).SetString(delegationAmount)
		divisor := new(big.Float).SetFloat64(1e18)
		delegatePC := new(big.Float).Quo(delegateFloat, divisor)
		p.KeyValueLine("Amount Delegated", fmt.Sprintf("%.6f", delegatePC)+" PC", "yellow")
		fmt.Println()

		// Show helpful next steps
		fmt.Println(p.Colors.SubHeader("Next Steps"))
		fmt.Println(p.Colors.Separator(40))
		fmt.Println()
		fmt.Println(p.Colors.Info("  1. Check updated validator status:"))
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "     push-validator validators"))
		fmt.Println()
		fmt.Println(p.Colors.Info("  2. View dashboard:"))
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "     push-validator dashboard"))
		fmt.Println()
	}
}
