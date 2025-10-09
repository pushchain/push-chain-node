package main

import (
	"bufio"
	"context"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/config"
	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/node"
	ui "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/ui"
	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/validator"
	"golang.org/x/term"
)

// handleRegisterValidator is a compatibility wrapper that pulls
// defaults from env and invokes runRegisterValidator.
// It prompts interactively for moniker and key name if not set via env vars.
func handleRegisterValidator(cfg config.Config) {
	// Get defaults from env or use hardcoded fallbacks
	defaultMoniker := getenvDefault("MONIKER", "push-validator")
	defaultKeyName := getenvDefault("KEY_NAME", "validator-key")
	defaultAmount := getenvDefault("STAKE_AMOUNT", "1500000000000000000")

	moniker := defaultMoniker
	keyName := defaultKeyName

	// Interactive prompts (skip in JSON mode or if env vars are explicitly set)
	if flagOutput != "json" {
		savedStdin := os.Stdin
		var tty *os.File
		if !flagNonInteractive && !term.IsTerminal(int(savedStdin.Fd())) {
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

		if os.Getenv("MONIKER") == "" {
			fmt.Printf("Enter validator name (moniker) [%s]: ", defaultMoniker)
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)
			if input != "" {
				moniker = input
			}
			fmt.Println()
		}

		if os.Getenv("KEY_NAME") == "" {
			fmt.Printf("Enter key name for validator (default: %s): ", defaultKeyName)
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)
			if input != "" {
				keyName = input
			}

			// Check if key already exists
			if keyExists(cfg, keyName) {
				p := ui.NewPrinter(flagOutput)
				fmt.Println()
				fmt.Println(p.Colors.Warning(fmt.Sprintf("⚠ Key '%s' already exists.", keyName)))
				fmt.Println()
				fmt.Println(p.Colors.Info("You can use this existing key or create a new one."))
				fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "Note: Recovery mnemonics are only shown when creating new keys."))
				fmt.Printf("\nEnter a different key name (or press ENTER to use existing key): ")
				newName, _ := reader.ReadString('\n')
				newName = strings.TrimSpace(newName)
				if newName != "" {
					keyName = newName
				} else {
					// User chose to reuse existing key
					fmt.Println()
					fmt.Println(p.Colors.Success("✓ Proceeding with existing key"))
					fmt.Println()
				}
			}
			fmt.Println()
		}

		// Check if validator is already registered before asking for commission
		// Note: IsValidator checks consensus pubkey, not account address, so we don't need to create the key yet
		v := validator.NewWith(validator.Options{BinPath: findPchaind(), HomeDir: cfg.HomeDir, ChainID: cfg.ChainID, Keyring: cfg.KeyringBackend, GenesisDomain: cfg.GenesisDomain, Denom: cfg.Denom})
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		// Check if this node is already registered as a validator (pass empty address since we check consensus pubkey)
		isVal, err := v.IsValidator(ctx, "")
		if err != nil {
			// Network/query error - registration won't work anyway, fail fast
			p := ui.NewPrinter(flagOutput)
			fmt.Println()
			fmt.Println(p.Colors.Error("⚠️ Failed to verify validator status"))
			fmt.Printf("Error: %v\n\n", err)
			fmt.Println("Please check your network connection and genesis domain configuration.")
			return
		}
		if isVal {
			// Already registered - show status
			p := ui.NewPrinter(flagOutput)
			fmt.Println()
			fmt.Println(p.Colors.Error("❌ This node is already registered as a validator"))
			fmt.Println()
			fmt.Println("Your validator is active on the network.")
			fmt.Println()
			p.Section("Validator Status")
			fmt.Println()
			fmt.Println(p.Colors.Info("  Check your validator:"))
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "     push-validator-manager validators"))
			fmt.Println()
			fmt.Println(p.Colors.Info("  Monitor node status:"))
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "     push-validator-manager status"))
			fmt.Println()
			return
		}

		// Commission rate prompt (only if not already registered)
		var commissionRate string
		if os.Getenv("COMMISSION_RATE") == "" {
			p := ui.NewPrinter(flagOutput)
			fmt.Printf("Enter commission rate (1-100%%) [10]: ")
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)

			if input == "" {
				commissionRate = "0.10" // Default 10%
			} else {
				// Parse and validate
				rate, err := strconv.ParseFloat(input, 64)
				if err != nil || rate < 1 || rate > 100 {
					fmt.Println(p.Colors.Error("⚠ Invalid commission rate. Using default 10%"))
					commissionRate = "0.10"
				} else {
					// Convert percentage to decimal (e.g., 15 -> 0.15)
					commissionRate = fmt.Sprintf("%.2f", rate/100)
				}
			}
			fmt.Println()
		} else {
			commissionRate = getenvDefault("COMMISSION_RATE", "0.10")
		}

		runRegisterValidator(cfg, moniker, keyName, defaultAmount, commissionRate)
	} else {
		// JSON mode or env vars set
		commissionRate := getenvDefault("COMMISSION_RATE", "0.10")
		runRegisterValidator(cfg, moniker, keyName, defaultAmount, commissionRate)
	}
}

// keyExists checks if a key with the given name already exists in the keyring
func keyExists(cfg config.Config, keyName string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, findPchaind(), "keys", "show", keyName, "-a",
		"--keyring-backend", cfg.KeyringBackend, "--home", cfg.HomeDir)
	err := cmd.Run()
	return err == nil
}

// runRegisterValidator performs the end-to-end registration flow:
// - verify node is not catching up
// - ensure key exists
// - wait for funding if necessary
// - submit create-validator transaction
// It prints text or JSON depending on --output.
func runRegisterValidator(cfg config.Config, moniker, keyName, amount, commissionRate string) {
	savedStdin := os.Stdin
	var tty *os.File
	if !flagNonInteractive && !term.IsTerminal(int(savedStdin.Fd())) {
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

	local := strings.TrimRight(cfg.RPCLocal, "/")
	if local == "" {
		local = "http://127.0.0.1:26657"
	}
	remoteHTTP := "https://" + strings.TrimSuffix(cfg.GenesisDomain, "/") + ":443"
	cliLocal := node.New(local)
	cliRemote := node.New(remoteHTTP)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stLocal, err1 := cliLocal.Status(ctx)
	_, err2 := cliRemote.RemoteStatus(ctx, remoteHTTP)
	if err1 == nil && err2 == nil {
		if stLocal.CatchingUp {
			if flagOutput == "json" {
				getPrinter().JSON(map[string]any{"ok": false, "error": "node is still syncing"})
			} else {
				fmt.Println("node is still syncing. Run 'push-validator-manager sync' first")
			}
			return
		}
	}
	v := validator.NewWith(validator.Options{BinPath: findPchaind(), HomeDir: cfg.HomeDir, ChainID: cfg.ChainID, Keyring: cfg.KeyringBackend, GenesisDomain: cfg.GenesisDomain, Denom: cfg.Denom})
	ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel2()
	keyInfo, err := v.EnsureKey(ctx2, keyName)
	if err != nil {
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": err.Error()})
		} else {
			fmt.Printf("key error: %v\n", err)
		}
		return
	}

	evmAddr, err := v.GetEVMAddress(ctx2, keyInfo.Address)
	if err != nil {
		evmAddr = ""
	}

	p := ui.NewPrinter(flagOutput)

	if flagOutput != "json" {
		// Display mnemonic if this is a new key
		if keyInfo.Mnemonic != "" {
			// Display mnemonic in prominent box
			p.MnemonicBox(keyInfo.Mnemonic)
			fmt.Println()

			// Warning message in yellow
			fmt.Println(p.Colors.Warning("**Important** Write this mnemonic phrase in a safe place."))
			fmt.Println(p.Colors.Warning("It is the only way to recover your account if you ever forget your password."))
			fmt.Println()
		} else {
			// Existing key - show clear status with reminder
			fmt.Println(p.Colors.Success(fmt.Sprintf("✓ Using existing key: %s", keyInfo.Name)))
			fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "  (Recovery mnemonic was displayed when this key was first created)"))
			fmt.Println()
		}

		// Always display Account Info section (whether new or existing key)
		p.Section("Account Info")
		p.KeyValueLine("EVM Address", evmAddr, "blue")
		p.KeyValueLine("Cosmos Address", keyInfo.Address, "dim")
		fmt.Println()
	}
	const requiredBalance = "1600000000000000000"
	const stakeAmount = "1500000000000000000"
	maxRetries := 10

	for tries := 0; tries < maxRetries; {
		balCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		bal, err := v.Balance(balCtx, keyInfo.Address)
		cancel()
		if err != nil {
			fmt.Printf("⚠️ Balance check failed: %v\n", err)
			tries++
			time.Sleep(2 * time.Second)
			continue
		}
		balInt := new(big.Int)
		balInt.SetString(bal, 10)
		reqInt := new(big.Int)
		reqInt.SetString(requiredBalance, 10)
		if balInt.Cmp(reqInt) >= 0 {
			fmt.Println(p.Colors.Success("✅ Sufficient balance"))
			break
		}
		pcAmount := "0.000000"
		if bal != "0" {
			balFloat, _ := new(big.Float).SetString(bal)
			divisor := new(big.Float).SetFloat64(1e18)
			result := new(big.Float).Quo(balFloat, divisor)
			pcAmount = fmt.Sprintf("%.6f", result)
		}

		// Display funding information
		p.KeyValueLine("Current Balance", pcAmount+" PC", "yellow")
		p.KeyValueLine("Min Balance Required", "1.6 PC", "yellow")
		fmt.Println()
		fmt.Printf("Please send %s to the EVM address shown above.\n\n", p.Colors.Warning("1.6 PC"))
		fmt.Printf("Use faucet at %s for testnet validators\n", p.Colors.Info("https://faucet.push.org"))
		fmt.Printf("or contact us at %s\n\n", p.Colors.Info("push.org/support"))

		fmt.Print(p.Colors.Apply(p.Colors.Theme.Prompt, "Press ENTER after funding..."))
		reader := bufio.NewReader(os.Stdin)
		_, _ = reader.ReadString('\n')
	}
	stake := amount
	if stake == "" {
		stake = stakeAmount
	}
	// Create fresh context for registration transaction (independent of earlier operations)
	regCtx, regCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer regCancel()
	txHash, err := v.Register(regCtx, validator.RegisterArgs{Moniker: moniker, Amount: stake, KeyName: keyName, CommissionRate: commissionRate, MinSelfDelegation: "1"})
	if err != nil {
		errMsg := err.Error()
		if flagOutput == "json" {
			getPrinter().JSON(map[string]any{"ok": false, "error": errMsg})
		} else {
			// Check if this is a "validator already exists" error
			if strings.Contains(errMsg, "validator already exist") {
				p := getPrinter()
				fmt.Println()
				fmt.Println(p.Colors.Error("❌ Validator registration failed: Validator pubkey already exists"))
				fmt.Println()
				fmt.Println("This validator consensus key is already registered on the network.")
				fmt.Println()
				p.Section("Resolution Options")
				fmt.Println()
				fmt.Println(p.Colors.Info("  1. Check existing validators:"))
				fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "     push-validator-manager validators"))
				fmt.Println()
				fmt.Println(p.Colors.Info("  2. To register a new validator, reset node data:"))
				fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "     push-validator-manager reset"))
				fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "     (This will generate new validator keys)"))
				fmt.Println()
				fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "  Note: Resetting will create a new validator identity."))
				fmt.Println()
			} else {
				fmt.Printf("register error: %v\n", err)
			}
		}
		return
	}

	// Success output
	if flagOutput == "json" {
		getPrinter().JSON(map[string]any{"ok": true, "txhash": txHash, "moniker": moniker, "key_name": keyName, "commission_rate": commissionRate})
	} else {
		fmt.Println()
		p := getPrinter()
		p.Success("✅ Validator registration successful!")
		fmt.Println()

		// Display registration details
		p.KeyValueLine("Transaction Hash", txHash, "green")
		p.KeyValueLine("Validator Name", moniker, "blue")

		// Convert commission rate back to percentage for display
		commRate, _ := strconv.ParseFloat(commissionRate, 64)
		p.KeyValueLine("Commission Rate", fmt.Sprintf("%.0f%%", commRate*100), "dim")
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
		fmt.Println(p.Colors.Info("  3. View node logs:"))
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Command, "     push-validator-manager logs"))
		fmt.Println()
		fmt.Println(p.Colors.Apply(p.Colors.Theme.Description, "  Your validator will appear in the active set after the next epoch."))
		fmt.Println()
	}
	return
}
