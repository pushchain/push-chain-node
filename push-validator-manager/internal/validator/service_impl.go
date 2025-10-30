package validator

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "os/exec"
    "strings"
    "time"
)

type Options struct {
    BinPath       string
    HomeDir       string
    ChainID       string
    Keyring       string
    GenesisDomain string // e.g., rpc-testnet-donut-node1.push.org
    Denom         string // e.g., upc
}

func NewWith(opts Options) Service { return &svc{opts: opts} }

type svc struct { opts Options }

func (s *svc) EnsureKey(ctx context.Context, name string) (KeyInfo, error) {
    if name == "" { return KeyInfo{}, errors.New("key name required") }
    if s.opts.BinPath == "" { s.opts.BinPath = "pchaind" }

    // Check if key already exists
    show := exec.CommandContext(ctx, s.opts.BinPath, "keys", "show", name, "-a", "--keyring-backend", s.opts.Keyring, "--home", s.opts.HomeDir)
    out, err := show.Output()
    if err == nil {
        // Key exists - fetch details
        return s.getKeyInfo(ctx, name, strings.TrimSpace(string(out)), "")
    }

    // Key doesn't exist - create it and capture output
    add := exec.CommandContext(ctx, s.opts.BinPath, "keys", "add", name, "--keyring-backend", s.opts.Keyring, "--algo", "eth_secp256k1", "--home", s.opts.HomeDir)

    // Capture output to parse mnemonic
    output, err := add.CombinedOutput()
    if err != nil {
        return KeyInfo{}, fmt.Errorf("keys add: %w", err)
    }

    // Parse the output to extract mnemonic
    mnemonic := extractMnemonic(string(output))

    // Get the address
    out2, err := exec.CommandContext(ctx, s.opts.BinPath, "keys", "show", name, "-a", "--keyring-backend", s.opts.Keyring, "--home", s.opts.HomeDir).Output()
    if err != nil { return KeyInfo{}, fmt.Errorf("keys show: %w", err) }

    addr := strings.TrimSpace(string(out2))
    return s.getKeyInfo(ctx, name, addr, mnemonic)
}

// getKeyInfo fetches full key details
func (s *svc) getKeyInfo(ctx context.Context, name, addr, mnemonic string) (KeyInfo, error) {
    // Get key details in JSON format
    cmd := exec.CommandContext(ctx, s.opts.BinPath, "keys", "show", name, "--keyring-backend", s.opts.Keyring, "--home", s.opts.HomeDir, "-o", "json")
    out, err := cmd.Output()
    if err != nil {
        return KeyInfo{Address: addr, Name: name, Mnemonic: mnemonic}, nil
    }

    // Parse JSON to extract pubkey and type
    var keyData struct {
        Name    string `json:"name"`
        Type    string `json:"type"`
        Address string `json:"address"`
        Pubkey  struct {
            Type string `json:"@type"`
            Key  string `json:"key"`
        } `json:"pubkey"`
    }

    if err := json.Unmarshal(out, &keyData); err != nil {
        return KeyInfo{Address: addr, Name: name, Mnemonic: mnemonic}, nil
    }

    pubkeyJSON := fmt.Sprintf(`{"@type":"%s","key":"%s"}`, keyData.Pubkey.Type, keyData.Pubkey.Key)

    return KeyInfo{
        Address:  addr,
        Name:     keyData.Name,
        Pubkey:   pubkeyJSON,
        Type:     keyData.Type,
        Mnemonic: mnemonic,
    }, nil
}

// extractMnemonic extracts the mnemonic phrase from keys add output
func extractMnemonic(output string) string {
    lines := strings.Split(output, "\n")
    foundWarning := false

    // The mnemonic appears after the warning message, skip the warning line itself,
    // then skip empty lines, and the next non-empty line is the mnemonic
    for i, line := range lines {
        line = strings.TrimSpace(line)

        // Look for the warning message
        if strings.Contains(line, "write this mnemonic phrase") {
            foundWarning = true
            continue
        }

        // After finding the warning, skip empty lines and capture the next non-empty line
        if foundWarning {
            if line == "" {
                continue
            }
            // This is the mnemonic line (first non-empty line after the warning)
            // Make sure it's not another message line by checking if it starts with common message prefixes
            if !strings.HasPrefix(line, "**") && !strings.HasPrefix(line, "It is") && len(line) > 20 {
                return line
            }
            // If we hit "It is the only way..." or similar, look for the next line
            if i+1 < len(lines) {
                nextLine := strings.TrimSpace(lines[i+1])
                if nextLine != "" && len(nextLine) > 20 {
                    return nextLine
                }
            }
        }
    }

    return ""
}

func (s *svc) GetEVMAddress(ctx context.Context, addr string) (string, error) {
    if addr == "" { return "", errors.New("address required") }
    if s.opts.BinPath == "" { s.opts.BinPath = "pchaind" }
    cmd := exec.CommandContext(ctx, s.opts.BinPath, "debug", "addr", addr)
    out, err := cmd.Output()
    if err != nil { return "", fmt.Errorf("debug addr: %w", err) }
    // Parse output to extract hex address
    lines := strings.Split(string(out), "\n")
    for _, line := range lines {
        if strings.HasPrefix(line, "Address (hex):") {
            parts := strings.SplitN(line, ":", 2)
            if len(parts) == 2 {
                hex := strings.TrimSpace(parts[1])
                return "0x" + hex, nil
            }
        }
    }
    return "", errors.New("could not extract EVM address from debug output")
}

func (s *svc) IsValidator(ctx context.Context, addr string) (bool, error) {
    if s.opts.BinPath == "" { s.opts.BinPath = "pchaind" }
    // Compare local consensus pubkey with remote validators
    showVal := exec.CommandContext(ctx, s.opts.BinPath, "tendermint", "show-validator", "--home", s.opts.HomeDir)
    b, err := showVal.Output()
    if err != nil { return false, fmt.Errorf("show-validator: %w", err) }
    var pub struct{ Key string `json:"key"` }
    if err := json.Unmarshal(b, &pub); err != nil { return false, err }
    if pub.Key == "" { return false, errors.New("empty consensus pubkey") }
    // Query validators from remote
    remote := fmt.Sprintf("tcp://%s:26657", s.opts.GenesisDomain)
    q := exec.CommandContext(ctx, s.opts.BinPath, "query", "staking", "validators", "--node", remote, "-o", "json")
    vb, err := q.Output()
    if err != nil { return false, fmt.Errorf("query validators: %w", err) }
    // Remote uses "value" field, not "key"
    var payload struct{ Validators []struct{ ConsensusPubkey struct{ Value string `json:"value"` } `json:"consensus_pubkey"` } `json:"validators"` }
    if err := json.Unmarshal(vb, &payload); err != nil { return false, err }
    for _, v := range payload.Validators {
        if strings.EqualFold(v.ConsensusPubkey.Value, pub.Key) { return true, nil }
    }
    return false, nil
}

func (s *svc) Balance(ctx context.Context, addr string) (string, error) {
    if s.opts.BinPath == "" { s.opts.BinPath = "pchaind" }
    // Always query remote genesis node for canonical state during validator registration
    remote := fmt.Sprintf("tcp://%s:26657", s.opts.GenesisDomain)
    q := exec.CommandContext(ctx, s.opts.BinPath, "query", "bank", "balances", addr, "--node", remote, "-o", "json")
    out, err := q.Output()
    if err != nil { return "0", fmt.Errorf("query balance: %w", err) }
    var payload struct{ Balances []struct{ Denom, Amount string } `json:"balances"` }
    if err := json.Unmarshal(out, &payload); err != nil { return "0", err }
    for _, c := range payload.Balances { if c.Denom == s.opts.Denom { return c.Amount, nil } }
    return "0", nil
}

func (s *svc) Register(ctx context.Context, args RegisterArgs) (string, error) {
    if s.opts.BinPath == "" { s.opts.BinPath = "pchaind" }
    // Prepare validator JSON - use a separate timeout for this command
    showCtx, showCancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer showCancel()
    pubJSON, err := exec.CommandContext(showCtx, s.opts.BinPath, "tendermint", "show-validator", "--home", s.opts.HomeDir).Output()
    if err != nil { return "", fmt.Errorf("show-validator: %w", err) }
    tmp, err := os.CreateTemp("", "validator-*.json")
    if err != nil { return "", err }
    defer os.Remove(tmp.Name())
    val := map[string]any{
        "pubkey": json.RawMessage(strings.TrimSpace(string(pubJSON))),
        "amount": fmt.Sprintf("%s%s", args.Amount, s.opts.Denom),
        "moniker": args.Moniker,
        "identity": "",
        "website": "",
        "security": "",
        "details": "Push Chain Validator",
        "commission-rate": valueOr(args.CommissionRate, "0.10"),
        "commission-max-rate": "0.20",
        "commission-max-change-rate": "0.01",
        "min-self-delegation": valueOr(args.MinSelfDelegation, "1"),
    }
    enc := json.NewEncoder(tmp)
    enc.SetEscapeHTML(false)
    if err := enc.Encode(val); err != nil { return "", err }
    _ = tmp.Close()

    // Submit TX
    remote := fmt.Sprintf("tcp://%s:26657", s.opts.GenesisDomain)
    ctxTimeout, cancel := context.WithTimeout(ctx, 60*time.Second)
    defer cancel()
    cmd := exec.CommandContext(ctxTimeout, s.opts.BinPath, "tx", "staking", "create-validator", tmp.Name(),
        "--from", args.KeyName,
        "--chain-id", s.opts.ChainID,
        "--keyring-backend", s.opts.Keyring,
        "--home", s.opts.HomeDir,
        "--node", remote,
        "--gas=auto", "--gas-adjustment=1.3", fmt.Sprintf("--gas-prices=1000000000%s", s.opts.Denom),
        "--yes",
    )
    out, err := cmd.CombinedOutput()
    if err != nil {
        // Try to extract a clean reason
        msg := extractErrorLine(string(out))
        if msg == "" { msg = err.Error() }
        return "", errors.New(msg)
    }
    // Find txhash:
    lines := strings.Split(string(out), "\n")
    for _, ln := range lines {
        if strings.Contains(ln, "txhash:") {
            parts := strings.SplitN(ln, "txhash:", 2)
            if len(parts) == 2 { return strings.TrimSpace(parts[1]), nil }
        }
    }
    return "", errors.New("transaction submitted; txhash not found in output")
}

func extractErrorLine(s string) string {
    for _, l := range strings.Split(s, "\n") {
        if strings.Contains(l, "rpc error:") || strings.Contains(l, "failed to execute message") || strings.Contains(l, "insufficient") || strings.Contains(l, "unauthorized") {
            return l
        }
    }
    return ""
}

func valueOr(v, d string) string { if strings.TrimSpace(v) == "" { return d }; return v }

// Unjail submits an unjail transaction to restore a jailed validator
func (s *svc) Unjail(ctx context.Context, keyName string) (string, error) {
	if s.opts.BinPath == "" { s.opts.BinPath = "pchaind" }
	if keyName == "" { return "", errors.New("key name required") }

	// Submit unjail transaction
	remote := fmt.Sprintf("tcp://%s:26657", s.opts.GenesisDomain)
	ctxTimeout, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctxTimeout, s.opts.BinPath, "tx", "slashing", "unjail",
		"--from", keyName,
		"--chain-id", s.opts.ChainID,
		"--keyring-backend", s.opts.Keyring,
		"--home", s.opts.HomeDir,
		"--node", remote,
		"--gas=auto", "--gas-adjustment=1.3", fmt.Sprintf("--gas-prices=1000000000%s", s.opts.Denom),
		"--yes",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Try to extract a clean reason
		msg := extractErrorLine(string(out))
		if msg == "" { msg = err.Error() }
		return "", errors.New(msg)
	}

	// Find txhash
	lines := strings.Split(string(out), "\n")
	for _, ln := range lines {
		if strings.Contains(ln, "txhash:") {
			parts := strings.SplitN(ln, "txhash:", 2)
			if len(parts) == 2 { return strings.TrimSpace(parts[1]), nil }
		}
	}
	return "", errors.New("transaction submitted; txhash not found in output")
}

// WithdrawRewards submits a transaction to withdraw delegation rewards and optionally commission
func (s *svc) WithdrawRewards(ctx context.Context, validatorAddr string, keyName string, includeCommission bool) (string, error) {
	if s.opts.BinPath == "" { s.opts.BinPath = "pchaind" }
	if validatorAddr == "" { return "", errors.New("validator address required") }
	if keyName == "" { return "", errors.New("key name required") }

	remote := fmt.Sprintf("tcp://%s:26657", s.opts.GenesisDomain)

	// Build the withdraw rewards command using validator address directly
	args := []string{
		"tx", "distribution", "withdraw-rewards", validatorAddr,
		"--from", keyName,
		"--chain-id", s.opts.ChainID,
		"--keyring-backend", s.opts.Keyring,
		"--home", s.opts.HomeDir,
		"--node", remote,
		"--gas=auto", "--gas-adjustment=1.3", fmt.Sprintf("--gas-prices=1000000000%s", s.opts.Denom),
		"--yes",
	}

	// Add commission flag if requested
	if includeCommission {
		args = append(args, "--commission")
	}

	// Submit transaction
	ctxTimeout, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctxTimeout, s.opts.BinPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Extract and enhance error message
		msg := extractErrorLine(string(out))
		if msg == "" { msg = err.Error() }

		// Improve error messages for common cases
		msg = improveRewardErrorMessage(msg)
		return "", errors.New(msg)
	}

	// Find txhash
	lines := strings.Split(string(out), "\n")
	for _, ln := range lines {
		if strings.Contains(ln, "txhash:") {
			parts := strings.SplitN(ln, "txhash:", 2)
			if len(parts) == 2 { return strings.TrimSpace(parts[1]), nil }
		}
	}
	return "", errors.New("transaction submitted; txhash not found in output")
}

// improveRewardErrorMessage provides user-friendly error messages for common withdrawal failures
func improveRewardErrorMessage(msg string) string {
	msg = strings.ToLower(msg)

	if strings.Contains(msg, "no delegation distribution info") {
		return "No rewards to withdraw. This is normal for new validators that haven't earned any rewards yet."
	}
	if strings.Contains(msg, "insufficient") && strings.Contains(msg, "fee") {
		return "Insufficient balance to pay transaction fees. Check your account balance."
	}
	if strings.Contains(msg, "invalid coins") || strings.Contains(msg, "empty") {
		return "No rewards available to withdraw."
	}
	if strings.Contains(msg, "unauthorized") {
		return "Transaction signing failed. Check that the key exists and is accessible."
	}

	return msg
}

// Delegate performs delegation (staking more tokens) to a validator
func (s *svc) Delegate(ctx context.Context, args DelegateArgs) (string, error) {
	if s.opts.BinPath == "" {
		s.opts.BinPath = "pchaind"
	}
	if args.ValidatorAddress == "" {
		return "", errors.New("validator address required")
	}
	if args.Amount == "" {
		return "", errors.New("amount required")
	}

	// Submit delegation transaction
	remote := fmt.Sprintf("tcp://%s:26657", s.opts.GenesisDomain)
	ctxTimeout, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctxTimeout, s.opts.BinPath, "tx", "staking", "delegate",
		args.ValidatorAddress,
		fmt.Sprintf("%s%s", args.Amount, s.opts.Denom),
		"--from", args.KeyName,
		"--chain-id", s.opts.ChainID,
		"--keyring-backend", s.opts.Keyring,
		"--home", s.opts.HomeDir,
		"--node", remote,
		"--gas=auto", "--gas-adjustment=1.3", fmt.Sprintf("--gas-prices=1000000000%s", s.opts.Denom),
		"--yes",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		// Try to extract a clean error message
		msg := extractErrorLine(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}

	// Extract tx hash from output
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "txhash:") {
			parts := strings.SplitN(line, "txhash:", 2)
			if len(parts) > 1 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}

	return "", errors.New("delegation successful but transaction hash not found in output")
}
