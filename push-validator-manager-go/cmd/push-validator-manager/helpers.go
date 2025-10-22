package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/config"
	"github.com/pushchain/push-chain-node/push-validator-manager-go/internal/validator"
	ui "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/ui"
	"golang.org/x/term"
)

// findPchaind returns the path to the pchaind binary, resolving
// either PCHAIND or PCHAIN_BIN environment variables, or falling
// back to the literal "pchaind" on PATH.
func findPchaind() string {
    if v := os.Getenv("PCHAIND"); v != "" { return v }
    if v := os.Getenv("PCHAIN_BIN"); v != "" { return v }
    return "pchaind"
}

// getenvDefault returns the environment value for k, or default d
// when k is not set.
func getenvDefault(k, d string) string { if v := os.Getenv(k); v != "" { return v }; return d }

// getPrinter returns a UI printer bound to the current --output flag.
func getPrinter() ui.Printer { return ui.NewPrinter(flagOutput) }

// convertValidatorToAccountAddress converts a validator operator address (pushvaloper...)
// to its corresponding account address (push...) using pchaind debug addr
func convertValidatorToAccountAddress(validatorAddress string) (string, error) {
	bin := findPchaind()
	cmd := exec.Command(bin, "debug", "addr", validatorAddress)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to convert address: %w", err)
	}

	// Parse the output to find "Bech32 Acc: push1..."
	// Output format:
	// Address: [... bytes ...]
	// Address (hex): 6AD36CEE...
	// Bech32 Acc: push1dtfkemne22yusl2cn5y6lvewxwfk0a9rcs7rv6
	// Bech32 Val: pushvaloper1...
	// Bech32 Con: pushvalcons1...
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Bech32 Acc:") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				return parts[2], nil
			}
		}
	}

	return "", fmt.Errorf("could not find Bech32 Acc in debug output")
}

// findKeyNameByAddress finds the key name in the keyring that corresponds to the given address
func findKeyNameByAddress(cfg config.Config, accountAddress string) (string, error) {
	bin := findPchaind()
	cmd := exec.Command(bin, "keys", "list", "--keyring-backend", cfg.KeyringBackend, "--home", cfg.HomeDir, "--output", "json")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to list keys: %w", err)
	}

	// Parse the JSON output to find a key with matching address
	var keys []struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	}
	if err := json.Unmarshal(output, &keys); err != nil {
		return "", fmt.Errorf("failed to parse keys: %w", err)
	}

	// Find matching key
	for _, key := range keys {
		if key.Address == accountAddress {
			return key.Name, nil
		}
	}

	return "", fmt.Errorf("no key found for address %s", accountAddress)
}

// waitForSufficientBalance checks if the account has enough balance to pay gas fees
// If not, prompts user to fund the wallet and waits for them to press Enter
// requiredBalance is in micro-units (upc)
// Returns true if balance is sufficient, false if check failed
func waitForSufficientBalance(cfg config.Config, accountAddr string, requiredBalance string, operationName string) bool {
	p := ui.NewPrinter(flagOutput)
	v := validator.NewWith(validator.Options{
		BinPath:       findPchaind(),
		HomeDir:       cfg.HomeDir,
		ChainID:       cfg.ChainID,
		Keyring:       cfg.KeyringBackend,
		GenesisDomain: cfg.GenesisDomain,
		Denom:         cfg.Denom,
	})

	maxRetries := 10
	for tries := 0; tries < maxRetries; tries++ {
		balCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		bal, err := v.Balance(balCtx, accountAddr)
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
			fmt.Println()
			return true
		}

		// Convert balance to PC for display (1 PC = 1e18 upc)
		pcAmount := "0.000000"
		if bal != "0" {
			balFloat, _ := new(big.Float).SetString(bal)
			divisor := new(big.Float).SetFloat64(1e18)
			result := new(big.Float).Quo(balFloat, divisor)
			pcAmount = fmt.Sprintf("%.6f", result)
		}

		// Convert required to PC for display
		reqFloat, _ := new(big.Float).SetString(requiredBalance)
		divisor := new(big.Float).SetFloat64(1e18)
		reqPC := new(big.Float).Quo(reqFloat, divisor)
		reqPCStr := fmt.Sprintf("%.6f", reqPC)

		// Display funding information
		fmt.Println()
		p.KeyValueLine("Current Balance", pcAmount+" PC", "yellow")
		p.KeyValueLine("Min Balance Required", reqPCStr+" PC", "yellow")
		fmt.Println()
		fmt.Printf("Please send %s to your account for %s transaction fees.\n\n", p.Colors.Warning(reqPCStr+" PC"), operationName)
		fmt.Printf("Use faucet at %s for testnet validators\n", p.Colors.Info("https://faucet.push.org"))
		fmt.Printf("or contact us at %s\n\n", p.Colors.Info("push.org/support"))

		// Wait for user to press Enter
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
			fmt.Print(p.Colors.Apply(p.Colors.Theme.Prompt, "Press ENTER after funding..."))
			_, _ = reader.ReadString('\n')
			fmt.Println()
		}
	}

	// After max retries, give up
	fmt.Println(p.Colors.Error("❌ Unable to verify sufficient balance after multiple attempts"))
	fmt.Println()
	return false
}
