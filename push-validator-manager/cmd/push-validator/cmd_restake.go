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
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/node"
	ui "github.com/pushchain/push-chain-node/push-validator-manager/internal/ui"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/validator"
	"golang.org/x/term"
)

// handleRestakeAll orchestrates the restake-all flow:
// - verify node is synced
// - verify validator is registered
// - display current rewards
// - automatically withdraw all rewards (commission + outstanding)
// - ask for confirmation to restake with edit/cancel options
// - submit delegation transaction
// - display results
func handleRestakeAll(cfg config.Config) {
	p := ui.NewPrinter(flagOutput)

	if flagOutput != "json" {
		fmt.Println()
		p.Header("Push Validator Manager - Restake All Rewards")
		fmt.Println()
	}

	// Step 1: Check sync status
	if flagOutput != "json" {
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
			fmt.Println(p.Colors.Info("Please wait for sync to complete before restaking."))
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
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "  push-validator register"))
			fmt.Println()
		}
		return
	}

	if flagOutput != "json" {
		fmt.Println(" " + p.Colors.Success("✓"))
	}

	// Step 3: Fetch current rewards
	if flagOutput != "json" {
		fmt.Print(p.Colors.Apply(p.Colors.Theme.Prompt, "💰 Fetching current rewards..."))
	}

	ctx3, cancel3 := context.WithTimeout(context.Background(), 5*time.Second)
	commission, outstanding, rewardsErr := validator.GetValidatorRewards(ctx3, cfg, myVal.Address)
	cancel3()

	if flagOutput != "json" {
		fmt.Println(" " + p.Colors.Success("✓"))
	}

	if rewardsErr != nil {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": "failed to fetch rewards"})
		} else {
			fmt.Println()
			fmt.Println(p.Colors.Error("❌ Failed to fetch rewards"))
			fmt.Println()
			fmt.Printf("Error: %v\n", rewardsErr)
			fmt.Println()
		}
		return
	}

	// Display rewards summary
	if flagOutput != "json" {
		fmt.Println()
		p.Section("Current Rewards")
		p.KeyValueLine("Commission Rewards", commission+" PC", "green")
		p.KeyValueLine("Outstanding Rewards", outstanding+" PC", "green")
		fmt.Println()
	}

	// Parse rewards to check if any are available
	commissionFloat, _ := strconv.ParseFloat(strings.TrimSpace(commission), 64)
	outstandingFloat, _ := strconv.ParseFloat(strings.TrimSpace(outstanding), 64)
	totalRewards := commissionFloat + outstandingFloat
	const rewardThreshold = 0.01 // Minimum 0.01 PC to be worthwhile

	if totalRewards < rewardThreshold {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": "no significant rewards available"})
		} else {
			fmt.Println(p.Colors.Warning("⚠️ No significant rewards available (less than 0.01 PC)"))
			fmt.Println()
			fmt.Println(p.Colors.Info("Nothing to restake. Continue earning rewards and try again later."))
			fmt.Println()
		}
		return
	}

	// Step 4: Auto-detect key name from validator
	defaultKeyName := getenvDefault("KEY_NAME", "validator-key")
	var keyName string

	if myVal.Address != "" {
		accountAddr, convErr := convertValidatorToAccountAddress(myVal.Address)
		if convErr == nil {
			if foundKey, findErr := findKeyNameByAddress(cfg, accountAddr); findErr == nil {
				keyName = foundKey
				if flagOutput != "json" {
					fmt.Printf("🔑 Using key: %s\n", keyName)
					fmt.Println()
				}
			} else {
				keyName = defaultKeyName
			}
		} else {
			keyName = defaultKeyName
		}
	} else {
		keyName = defaultKeyName
	}

	// Step 5: Submit withdraw rewards transaction (always include commission for restaking)
	if flagOutput != "json" {
		fmt.Print(p.Colors.Apply(p.Colors.Theme.Prompt, "💸 Withdrawing all rewards..."))
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
	txHash, withdrawErr := v.WithdrawRewards(ctx5, myVal.Address, keyName, true) // Always include commission
	cancel5()

	if withdrawErr != nil {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": withdrawErr.Error(), "step": "withdraw"})
		} else {
			fmt.Println()
			fmt.Println(p.Colors.Error("❌ Withdrawal transaction failed"))
			fmt.Println()
			fmt.Printf("Error: %v\n", withdrawErr)
			fmt.Println()
		}
		return
	}

	if flagOutput != "json" {
		fmt.Println(" " + p.Colors.Success("✓"))
		fmt.Println()
		p.KeyValueLine("Transaction Hash", txHash, "green")
		fmt.Printf(p.Colors.Success("✓ Successfully withdrew %.6f PC\n"), totalRewards)
		fmt.Println()
	}

	// Step 6: Calculate available amount for restaking
	const feeReserve = 0.15 // Reserve 0.15 PC for gas fees
	maxRestakeable := totalRewards - feeReserve

	if maxRestakeable <= 0 {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{
				"ok":              true,
				"withdraw_txhash": txHash,
				"withdrawn":       fmt.Sprintf("%.6f", totalRewards),
				"restaked":        "0",
				"message":         "insufficient balance for restaking after gas reserve",
			})
		} else {
			fmt.Println(p.Colors.Warning("⚠️ Insufficient balance for restaking after gas reserve"))
			fmt.Println()
			fmt.Println("Funds have been withdrawn to your wallet but are too small to restake.")
			fmt.Println()
		}
		return
	}

	// Step 7: Display restaking options
	if flagOutput != "json" {
		p.Section("Available for Restaking")
		p.KeyValueLine("Withdrawn Amount", fmt.Sprintf("%.6f PC", totalRewards), "blue")
		p.KeyValueLine("Gas Reserve", fmt.Sprintf("%.2f PC", feeReserve), "dim")
		p.KeyValueLine("Available to Stake", fmt.Sprintf("%.6f PC", maxRestakeable), "blue")
		fmt.Println()
	}

	// Step 8: Interactive confirmation with edit/cancel option
	restakeAmount := maxRestakeable
	restakeAmountWei := ""

	if !flagNonInteractive && !flagYes && flagOutput != "json" {
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

		for {
			fmt.Printf("Restake %.6f PC? (y/n/edit) [y]: ", restakeAmount)
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(strings.ToLower(input))

			if input == "" || input == "y" || input == "yes" {
				// Proceed with full amount
				break
			} else if input == "n" || input == "no" {
				// Cancel restaking
				fmt.Println()
				fmt.Println(p.Colors.Info("Restaking cancelled. Funds remain in your wallet."))
				fmt.Println()
				if flagOutput == "json" {
					getPrinter().JSON(map[string]any{
						"ok":              true,
						"withdraw_txhash": txHash,
						"withdrawn":       fmt.Sprintf("%.6f", totalRewards),
						"restaked":        "0",
						"cancelled":       true,
					})
				}
				return
			} else if input == "edit" || input == "e" {
				// Allow user to edit amount
				fmt.Println()
				for {
					fmt.Printf("Enter amount to restake (0.01 - %.6f PC): ", maxRestakeable)
					amountInput, _ := reader.ReadString('\n')
					amountInput = strings.TrimSpace(amountInput)

					if amountInput == "" {
						fmt.Println(p.Colors.Error("⚠ Amount is required. Try again."))
						continue
					}

					// Parse user input
					customAmount, parseErr := strconv.ParseFloat(amountInput, 64)
					if parseErr != nil {
						fmt.Println(p.Colors.Error("⚠ Invalid amount. Enter a number. Try again."))
						continue
					}

					// Validate bounds
					if customAmount < 0.01 {
						fmt.Println(p.Colors.Error("⚠ Amount too low. Minimum restake is 0.01 PC. Try again."))
						continue
					}
					if customAmount > maxRestakeable {
						fmt.Printf(p.Colors.Error("⚠ Insufficient balance. Maximum: %.6f PC. Try again.\n"), maxRestakeable)
						continue
					}

					// Use custom amount
					restakeAmount = customAmount
					fmt.Printf(p.Colors.Success("✓ Will restake %.6f PC\n"), restakeAmount)
					fmt.Println()
					break
				}
				break
			} else {
				// Treat any other input as cancel
				fmt.Println()
				fmt.Println(p.Colors.Info("Invalid input. Restaking cancelled."))
				fmt.Println()
				return
			}
		}
	}

	// Convert to wei
	restakeWei := new(big.Float).Mul(new(big.Float).SetFloat64(restakeAmount), new(big.Float).SetFloat64(1e18))
	restakeAmountWei = restakeWei.Text('f', 0)

	// Step 9: Submit delegation transaction
	if flagOutput != "json" {
		fmt.Print(p.Colors.Apply(p.Colors.Theme.Prompt, "📤 Restaking funds..."))
	}

	ctx6, cancel6 := context.WithTimeout(context.Background(), 90*time.Second)
	delegateTxHash, delegateErr := v.Delegate(ctx6, validator.DelegateArgs{
		ValidatorAddress: myVal.Address,
		Amount:           restakeAmountWei,
		KeyName:          keyName,
	})
	cancel6()

	if delegateErr != nil {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{
				"ok":              false,
				"withdraw_txhash": txHash,
				"withdrawn":       fmt.Sprintf("%.6f", totalRewards),
				"restake_error":   delegateErr.Error(),
				"step":            "restake",
			})
		} else {
			fmt.Println()
			fmt.Println(p.Colors.Error("❌ Restaking transaction failed"))
			fmt.Println()
			fmt.Printf("Error: %v\n", delegateErr)
			fmt.Println()
			fmt.Println(p.Colors.Warning("Note: Rewards were successfully withdrawn. Funds are in your wallet."))
			fmt.Println(p.Colors.Info("You can manually delegate using: push-validator increase-stake"))
			fmt.Println()
		}
		return
	}

	if flagOutput != "json" {
		fmt.Println(" " + p.Colors.Success("✓"))
	}

	// Success output
	if flagOutput == "json" {
		getPrinter().JSON(map[string]any{
			"ok":                true,
			"withdraw_txhash":   txHash,
			"restake_txhash":    delegateTxHash,
			"withdrawn":         fmt.Sprintf("%.6f", totalRewards),
			"restaked":          fmt.Sprintf("%.6f", restakeAmount),
		})
	} else {
		fmt.Println()
		p.Success("✅ Successfully restaked rewards!")
		fmt.Println()

		// Display transaction details
		p.KeyValueLine("Withdrawal TxHash", txHash, "green")
		p.KeyValueLine("Restake TxHash", delegateTxHash, "green")
		p.KeyValueLine("Amount Restaked", fmt.Sprintf("%.6f PC", restakeAmount), "yellow")
		fmt.Println()

		// Show helpful next steps
		fmt.Println(p.Colors.SubHeader("Next Steps"))
		fmt.Println(p.Colors.Separator(40))
		fmt.Println()
		fmt.Println(p.Colors.Info("  1. Check your increased stake:"))
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "     push-validator status"))
		fmt.Println()
		fmt.Println(p.Colors.Info("  2. Monitor validator performance:"))
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "     push-validator dashboard"))
		fmt.Println()
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "  Your validator power has been increased!"))
		fmt.Println()
	}
}
