package main

import (
    "context"
    "fmt"
    "os/exec"
    "sort"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/pushchain/push-chain-node/push-validator-manager/internal/config"
    "github.com/pushchain/push-chain-node/push-validator-manager/internal/dashboard"
    ui "github.com/pushchain/push-chain-node/push-validator-manager/internal/ui"
    "github.com/pushchain/push-chain-node/push-validator-manager/internal/validator"
)

// truncateAddress truncates long addresses while keeping prefix and suffix visible
func truncateAddress(addr string, maxWidth int) string {
    if len(addr) <= maxWidth {
        return addr
    }
    if strings.HasPrefix(addr, "pushvaloper") {
        prefix := addr[:14]
        suffix := addr[len(addr)-8:]
        return prefix + "..." + suffix
    }
    if strings.HasPrefix(addr, "0x") || strings.HasPrefix(addr, "0X") {
        prefix := addr[:6]
        suffix := addr[len(addr)-6:]
        return prefix + "..." + suffix
    }
    return addr
}

func handleValidators(cfg config.Config) error {
    return handleValidatorsWithFormat(cfg, false)
}

// handleValidatorsWithFormat prints either a pretty table (default)
// or raw JSON (--output=json at root) of the current validator set.
func handleValidatorsWithFormat(cfg config.Config, jsonOut bool) error {
    // For JSON output, query raw data directly (matches chain's native format)
    if jsonOut {
        bin := findPchaind()
        remote := fmt.Sprintf("https://%s", cfg.GenesisDomain)
        ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()
        cmd := exec.CommandContext(ctx, bin, "query", "staking", "validators", "--node", remote, "-o", "json")
        output, err := cmd.Output()
        if err != nil {
            if ctx.Err() == context.DeadlineExceeded {
                return fmt.Errorf("validators: timeout connecting to %s", cfg.GenesisDomain)
            }
            return fmt.Errorf("validators: %w", err)
        }
        // passthrough raw JSON
        fmt.Println(string(output))
        return nil
    }

    // For table output, use cached fetcher (same approach as dashboard)
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()

    valList, err := validator.GetCachedValidatorsList(ctx, cfg)
    if err != nil {
        return fmt.Errorf("validators: %w", err)
    }

    if valList.Total == 0 {
        fmt.Println("No validators found or node not synced")
        return nil
    }

    // Fetch my validator info to highlight in table
    myValidatorAddr := ""
    myValCtx, myValCancel := context.WithTimeout(context.Background(), 10*time.Second)
    if myVal, err := validator.GetCachedMyValidator(myValCtx, cfg); err == nil {
        myValidatorAddr = myVal.Address
    }
    myValCancel()

    type validatorDisplay struct {
        moniker        string
        status         string
        statusOrder    int
        jailed         bool
        tokensPC       float64
        commissionPct  float64
        operatorAddr   string
        cosmosAddr     string
        commissionRwd  string
        outstandingRwd string
        evmAddress     string
        isMyValidator  bool
    }
    vals := make([]validatorDisplay, len(valList.Validators))
    var wg sync.WaitGroup

    for i, v := range valList.Validators {
        vals[i] = validatorDisplay{
            moniker:       v.Moniker,
            operatorAddr:  v.OperatorAddress,
            cosmosAddr:    v.OperatorAddress,
            jailed:        v.Jailed,
            isMyValidator: myValidatorAddr != "" && v.OperatorAddress == myValidatorAddr,
        }
        if vals[i].moniker == "" {
            vals[i].moniker = "unknown"
        }

        // Status is already converted (BONDED, UNBONDING, UNBONDED)
        switch v.Status {
        case "BONDED":
            vals[i].status, vals[i].statusOrder = "BONDED", 1
        case "UNBONDING":
            vals[i].status, vals[i].statusOrder = "UNBONDING", 2
        case "UNBONDED":
            vals[i].status, vals[i].statusOrder = "UNBONDED", 3
        default:
            vals[i].status, vals[i].statusOrder = v.Status, 4
        }

        // Parse tokens to PC
        if v.Tokens != "" && v.Tokens != "0" {
            if t, err := strconv.ParseFloat(v.Tokens, 64); err == nil {
                vals[i].tokensPC = t / 1e18
            }
        }

        // Parse commission percentage (v.Commission is already "XX%" format, extract the number)
        if v.Commission != "" && v.Commission != "0%" {
            commStr := strings.TrimSuffix(v.Commission, "%")
            if c, err := strconv.ParseFloat(commStr, 64); err == nil {
                vals[i].commissionPct = c
            }
        }

        // Fetch rewards and EVM address in parallel using goroutines
        wg.Add(1)
        go func(idx int, addr string) {
            defer wg.Done()
            // 3 second timeout per validator to avoid blocking
            fetchCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
            defer cancel()

            vals[idx].commissionRwd, vals[idx].outstandingRwd, _ = validator.GetValidatorRewards(fetchCtx, cfg, addr)
            vals[idx].evmAddress = validator.GetEVMAddress(fetchCtx, addr)
        }(i, v.OperatorAddress)
    }

    wg.Wait()
    sort.Slice(vals, func(i, j int) bool {
        // My validator always comes first
        if vals[i].isMyValidator != vals[j].isMyValidator {
            return vals[i].isMyValidator
        }
        if vals[i].statusOrder != vals[j].statusOrder { return vals[i].statusOrder < vals[j].statusOrder }
        return vals[i].tokensPC > vals[j].tokensPC
    })
    c := ui.NewColorConfig()
    fmt.Println()
    fmt.Println(c.Header(" 👥 Active Push Chain Validators "))
    headers := []string{"VALIDATOR", "COSMOS_ADDR", "STATUS", "STAKE(PC)", "COMM%", "COMM_RWD", "OUTSTND_RWD", "EVM_ADDR"}
    rows := make([][]string, 0, len(vals))
    for _, v := range vals {
        // Check if this is my validator
        moniker := v.moniker
        if v.isMyValidator {
            moniker = moniker + " [My Validator]"
        }

        // Build status string with optional (JAILED) suffix
        statusStr := v.status
        if v.jailed {
            statusStr = statusStr + " (JAILED)"
        }

        row := []string{
            moniker,
            truncateAddress(v.cosmosAddr, 24),
            statusStr,
            dashboard.FormatLargeNumber(int64(v.tokensPC)),
            fmt.Sprintf("%.0f%%", v.commissionPct),
            dashboard.FormatSmartNumber(v.commissionRwd),
            dashboard.FormatSmartNumber(v.outstandingRwd),
            truncateAddress(v.evmAddress, 16),
        }

        // Apply green highlighting to the entire row if it's my validator
        if v.isMyValidator {
            for i := range row {
                row[i] = c.Success(row[i])
            }
        }

        rows = append(rows, row)
    }
    fmt.Print(ui.Table(c, headers, rows, nil))
    fmt.Printf("Total Validators: %d\n", len(vals))
    fmt.Println(c.Info("💡 Tip: Use --output=json for full addresses and raw data"))
    return nil
}
