package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pushchain/push-chain-node/push-validator-manager/internal/config"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/node"
	ui "github.com/pushchain/push-chain-node/push-validator-manager/internal/ui"
	"github.com/pushchain/push-chain-node/push-validator-manager/internal/validator"
	"golang.org/x/term"
)

// handleUnjail orchestrates the validator unjail flow:
// - verify node is synced
// - verify validator is jailed with expired jail period
// - prompt for key name
// - submit unjail transaction
// - display results
func handleUnjail(cfg config.Config) {
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
			fmt.Println(p.Colors.Info("Please wait for sync to complete before unjailing."))
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "  push-validator-manager sync"))
			fmt.Println()
		}
		return
	}

	if flagOutput != "json" {
		fmt.Println(" " + p.Colors.Success("✓"))
	}

	// Step 2: Check validator jail status
	if flagOutput != "json" {
		fmt.Print(p.Colors.Apply(p.Colors.Theme.Prompt, "🔍 Checking validator jail status..."))
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
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "  push-validator-manager register-validator"))
			fmt.Println()
		}
		return
	}

	if !myVal.Jailed {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": "validator is not jailed"})
		} else {
			fmt.Println()
			fmt.Println(p.Colors.Success("✓ Validator is active (not jailed)"))
			fmt.Println()
			fmt.Println(p.Colors.Info("Status: " + myVal.Status))
			fmt.Println()
		}
		return
	}

	if flagOutput != "json" {
		fmt.Println(" " + p.Colors.Success("✓"))
	}

	// Step 3: Check if jail period has expired
	if flagOutput != "json" {
		fmt.Print(p.Colors.Apply(p.Colors.Theme.Prompt, "🔍 Checking jail expiry..."))
	}

	jailedUntil := myVal.SlashingInfo.JailedUntil
	if jailedUntil == "" {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": "could not determine jail period"})
		} else {
			fmt.Println()
			fmt.Println(p.Colors.Error("❌ Could not determine jail period"))
			fmt.Println()
		}
		return
	}

	// Check if jail time has passed
	if !isJailPeriodExpired(jailedUntil) {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": "jail period has not expired", "jailed_until": jailedUntil})
		} else {
			fmt.Println()
			fmt.Println(p.Colors.Warning("⚠️ Jail period has not expired yet"))
			fmt.Println()
			fmt.Printf("Jailed until: %s\n", jailedUntil)
			fmt.Println()
			fmt.Println(p.Colors.Info("Please wait until the jail period expires before attempting to unjail."))
			fmt.Println()
		}
		return
	}

	if flagOutput != "json" {
		fmt.Println(" " + p.Colors.Success("✓"))
	}

	// Step 4: Auto-derive key name from validator
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
		fmt.Printf("\nEnter key name for unjailing [%s]: ", defaultKeyName)
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
		if !waitForSufficientBalance(cfg, accountAddr, requiredForGasFees, "unjail") {
			return
		}
	}

	// Step 6: Submit unjail transaction
	if flagOutput != "json" {
		fmt.Print(p.Colors.Apply(p.Colors.Theme.Prompt, "📤 Submitting unjail transaction..."))
	}

	v := validator.NewWith(validator.Options{
		BinPath:       findPchaind(),
		HomeDir:       cfg.HomeDir,
		ChainID:       cfg.ChainID,
		Keyring:       cfg.KeyringBackend,
		GenesisDomain: cfg.GenesisDomain,
		Denom:         cfg.Denom,
	})

	ctx3, cancel3 := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel3()

	txHash, err := v.Unjail(ctx3, keyName)
	if err != nil {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": err.Error()})
		} else {
			fmt.Println()
			fmt.Println(p.Colors.Error("❌ Unjail transaction failed"))
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
		p.Success("✅ Validator successfully unjailed!")
		fmt.Println()

		// Display transaction hash
		p.KeyValueLine("Transaction Hash", txHash, "green")
		fmt.Println()

		// Show helpful next steps
		fmt.Println(p.Colors.SubHeader("Next Steps"))
		fmt.Println(p.Colors.Separator(40))
		fmt.Println()
		fmt.Println(p.Colors.Info("  1. Check validator status:"))
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "     push-validator-manager validators"))
		fmt.Println()
		fmt.Println(p.Colors.Info("  2. Monitor node status:"))
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "     push-validator-manager status"))
		fmt.Println()
		fmt.Println(p.Colors.Info("  3. Live dashboard:"))
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "     push-validator-manager dashboard"))
		fmt.Println()
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "  Your validator will resume block signing and earning rewards."))
		fmt.Println()
	}
}

// isJailPeriodExpired checks if the jail period has passed
func isJailPeriodExpired(jailedUntil string) bool {
	if jailedUntil == "" || jailedUntil == "1970-01-01T00:00:00Z" {
		return true // No jail time means expired
	}

	t, err := time.Parse(time.RFC3339Nano, jailedUntil)
	if err != nil {
		return false // If we can't parse, assume not expired
	}

	return time.Now().After(t)
}
