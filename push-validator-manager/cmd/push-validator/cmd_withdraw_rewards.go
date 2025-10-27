package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pushchain/push-chain-node/push-validator-manager/internal/config"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/node"
	ui "github.com/pushchain/push-chain-node/push-validator-manager/internal/ui"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/validator"
	"golang.org/x/term"
)

// handleWithdrawRewards orchestrates the withdraw rewards flow:
// - verify node is synced
// - verify validator is registered
// - display current rewards
// - prompt for key name
// - ask about including commission
// - submit withdraw transaction
// - display results
func handleWithdrawRewards(cfg config.Config) {
	p := ui.NewPrinter(flagOutput)

	// Step 1: Check sync status
	if flagOutput != "json" {
		fmt.Println()
		fmt.Print(p.Colors.Apply(p.Colors.Theme.Prompt, "🔍 Checking node sync status..."))
	}

	local := strings.TrimRight(cfg.RPCLocal, "/")
	if local == "" {
		local = "http://127.0.0.1:26657"
	}
	remoteHTTP := "https://" + strings.TrimSuffix(cfg.GenesisDomain, "/") + ":443"
	cliLocal := node.New(local)
	cliRemote := node.New(remoteHTTP)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	stLocal, err1 := cliLocal.Status(ctx)
	_, err2 := cliRemote.RemoteStatus(ctx, remoteHTTP)
	cancel()

	if err1 != nil || err2 != nil {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": "failed to check sync status"})
		} else {
			fmt.Println()
			fmt.Println(p.Colors.Error("❌ Failed to check sync status"))
			fmt.Println()
			fmt.Println(p.Colors.Info("Please verify your node is running and properly configured."))
			fmt.Println()
		}
		return
	}

	if stLocal.CatchingUp {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": "node is still syncing"})
		} else {
			fmt.Println()
			fmt.Println(p.Colors.Warning("⚠️ Node is still syncing to latest block"))
			fmt.Println()
			fmt.Println(p.Colors.Info("Please wait for sync to complete before withdrawing rewards."))
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "  push-validator sync"))
			fmt.Println()
		}
		return
	}

	if flagOutput != "json" {
		fmt.Println(" " + p.Colors.Success("✓"))
	}

	// Step 2: Check validator registration
	if flagOutput != "json" {
		fmt.Print(p.Colors.Apply(p.Colors.Theme.Prompt, "🔍 Checking validator status..."))
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	myVal, statusErr := validator.GetCachedMyValidator(ctx2, cfg)
	cancel2()

	if statusErr != nil {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": "failed to check validator status"})
		} else {
			fmt.Println()
			fmt.Println(p.Colors.Error("❌ Failed to check validator status"))
			fmt.Println()
		}
		return
	}

	if !myVal.IsValidator {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": "node is not registered as validator"})
		} else {
			fmt.Println()
			fmt.Println(p.Colors.Warning("⚠️ This node is not registered as a validator"))
			fmt.Println()
			fmt.Println(p.Colors.Info("Register first using:"))
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "  push-validator register-validator"))
			fmt.Println()
		}
		return
	}

	if flagOutput != "json" {
		fmt.Println(" " + p.Colors.Success("✓"))
	}

	// Step 3: Display current rewards
	if flagOutput != "json" {
		fmt.Print(p.Colors.Apply(p.Colors.Theme.Prompt, "💰 Fetching current rewards..."))
	}

	ctx3, cancel3 := context.WithTimeout(context.Background(), 5*time.Second)
	commission, outstanding, rewardsErr := validator.GetValidatorRewards(ctx3, cfg, myVal.Address)
	cancel3()

	if flagOutput != "json" {
		fmt.Println(" " + p.Colors.Success("✓"))
	}

	if flagOutput == "json" {
		getPrinter().JSON(map[string]any{
			"ok":                  true,
			"commission_rewards":  commission,
			"outstanding_rewards": outstanding,
		})
		return
	}

	// Display rewards summary and validate
	fmt.Println()
	p.Header("Current Rewards")
	if rewardsErr == nil {
		p.KeyValueLine("Commission Rewards", commission+" PC", "green")
		p.KeyValueLine("Outstanding Rewards", outstanding+" PC", "green")
	} else {
		fmt.Println(p.Colors.Warning("⚠️ Could not fetch rewards, but proceeding with withdrawal"))
	}
	fmt.Println()

	// Parse rewards to check if any are available
	commissionFloat, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(commission, "PC")), 64)
	outstandingFloat, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(outstanding, "PC")), 64)
	const rewardThreshold = 0.01 // Minimum 0.01 PC to be worthwhile
	hasSignificantRewards := commissionFloat >= rewardThreshold || outstandingFloat >= rewardThreshold

	// Warn if rewards are minimal
	if !hasSignificantRewards && rewardsErr == nil {
		fmt.Println(p.Colors.Warning("⚠️ No significant rewards available (less than 0.01 PC)"))
		if !flagNonInteractive {
			savedStdin := os.Stdin
			var tty *os.File
			if !term.IsTerminal(int(savedStdin.Fd())) {
				if t, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0); err == nil {
					tty = t
					os.Stdin = t
				}
			}
			if tty != nil {
				defer func() {
					os.Stdin = savedStdin
					tty.Close()
				}()
			}

			reader := bufio.NewReader(os.Stdin)
			fmt.Print("Continue with withdrawal anyway? (y/N): ")
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(strings.ToLower(input))
			if input != "y" && input != "yes" {
				fmt.Println()
				fmt.Println(p.Colors.Info("Withdrawal cancelled."))
				fmt.Println()
				return
			}
			fmt.Println()
		} else {
			// Non-interactive: abort if no rewards
			fmt.Println(p.Colors.Error("❌ No significant rewards to withdraw. Aborting."))
			fmt.Println()
			return
		}
	}

	// Step 4: Auto-detect key name from validator
	defaultKeyName := getenvDefault("KEY_NAME", "validator-key")
	var keyName string

	// Try to auto-derive the key name from the validator's address
	if myVal.Address != "" {
		// Convert validator address to account address
		accountAddr, convErr := convertValidatorToAccountAddress(myVal.Address)
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

	// Only prompt if explicitly requested via env or interactive mode AND key derivation failed
	if flagOutput != "json" && !flagNonInteractive && keyName == defaultKeyName && os.Getenv("KEY_NAME") == "" {
		// Interactive prompt for key name
		savedStdin := os.Stdin
		var tty *os.File
		if !term.IsTerminal(int(savedStdin.Fd())) {
			if t, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0); err == nil {
				tty = t
				os.Stdin = t
			}
		}
		if tty != nil {
			defer func() {
				os.Stdin = savedStdin
				tty.Close()
			}()
		}

		reader := bufio.NewReader(os.Stdin)
		fmt.Printf("\nEnter key name for withdrawal [%s]: ", defaultKeyName)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input != "" {
			keyName = input
		} else {
			keyName = defaultKeyName
		}
		fmt.Println()
	}

	// Step 5: Check balance for gas fees
	if flagOutput != "json" {
		fmt.Print(p.Colors.Apply(p.Colors.Theme.Prompt, "💰 Checking wallet balance for gas fees..."))
	}

	// Convert validator address to account address for balance check
	accountAddr, addrErr := convertValidatorToAccountAddress(myVal.Address)
	if addrErr != nil {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": "failed to derive account address"})
		} else {
			fmt.Println()
			fmt.Println(p.Colors.Error("❌ Failed to derive account address"))
			fmt.Println()
		}
		return
	}

	if flagOutput != "json" {
		fmt.Println(" " + p.Colors.Success("✓"))
	}

	// Wait for sufficient balance (only in interactive mode)
	if flagOutput != "json" && !flagNonInteractive {
		const requiredForGasFees = "150000000000000000" // 0.15 PC in micro-units, enough for gas (actual: ~0.1037 PC + 1.45x buffer)
		if !waitForSufficientBalance(cfg, accountAddr, requiredForGasFees, "withdraw") {
			return
		}
	}

	// Step 7: Ask about commission
	var includeCommission bool
	if !flagNonInteractive {
		savedStdin := os.Stdin
		var tty *os.File
		if !term.IsTerminal(int(savedStdin.Fd())) {
			if t, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0); err == nil {
				tty = t
				os.Stdin = t
			}
		}
		if tty != nil {
			defer func() {
				os.Stdin = savedStdin
				tty.Close()
			}()
		}

		reader := bufio.NewReader(os.Stdin)
		fmt.Print("Include commission rewards in withdrawal? (y/n) [n]: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		includeCommission = input == "y" || input == "yes"
		fmt.Println()
	}

	// Step 8: Submit withdraw rewards transaction
	if flagOutput != "json" {
		fmt.Print(p.Colors.Apply(p.Colors.Theme.Prompt, "📤 Submitting withdrawal transaction..."))
	}

	v := validator.NewWith(validator.Options{
		BinPath:       findPchaind(),
		HomeDir:       cfg.HomeDir,
		ChainID:       cfg.ChainID,
		Keyring:       cfg.KeyringBackend,
		GenesisDomain: cfg.GenesisDomain,
		Denom:         cfg.Denom,
	})

	ctx5, cancel5 := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel5()

	txHash, err := v.WithdrawRewards(ctx5, myVal.Address, keyName, includeCommission)
	if err != nil {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": err.Error()})
		} else {
			fmt.Println()
			fmt.Println(p.Colors.Error("❌ Withdrawal transaction failed"))
			fmt.Println()
			fmt.Printf("Error: %v\n", err)
			fmt.Println()
		}
		return
	}

	if flagOutput != "json" {
		fmt.Println(" " + p.Colors.Success("✓"))
	}

	// Success output
	if flagOutput == "json" {
		getPrinter().JSON(map[string]any{"ok": true, "txhash": txHash})
	} else {
		fmt.Println()
		p.Success("✅ Rewards successfully withdrawn!")
		fmt.Println()

		// Display transaction hash
		p.KeyValueLine("Transaction Hash", txHash, "green")
		fmt.Println()

		// Show helpful next steps
		fmt.Println(p.Colors.SubHeader("Next Steps"))
		fmt.Println(p.Colors.Separator(40))
		fmt.Println()
		fmt.Println(p.Colors.Info("  1. Check validator status:"))
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "     push-validator validators"))
		fmt.Println()
		fmt.Println(p.Colors.Info("  2. View account balance:"))
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "     push-validator balance"))
		fmt.Println()
		fmt.Println(p.Colors.Info("  3. Live dashboard:"))
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "     push-validator dashboard"))
		fmt.Println()
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "  Your rewards have been transferred to your account."))
		fmt.Println()
	}
}
