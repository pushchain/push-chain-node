package main

import (
    "context"
    "encoding/json"
    "fmt"
    "os/exec"
    "sort"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/config"
    ui "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/ui"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/validator"
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
    bin := findPchaind()
    remote := fmt.Sprintf("tcp://%s:26657", cfg.GenesisDomain)
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    cmd := exec.CommandContext(ctx, bin, "query", "staking", "validators", "--node", remote, "-o", "json")
    output, err := cmd.Output()
    if err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            return fmt.Errorf("validators: timeout connecting to %s", cfg.GenesisDomain)
        }
        return fmt.Errorf("validators: %w", err)
    }
    if jsonOut {
        // passthrough raw JSON
        fmt.Println(string(output))
        return nil
    }
    var result struct {
        Validators []struct {
            Description struct {
                Moniker string `json:"moniker"`
                Details string `json:"details"`
            } `json:"description"`
            OperatorAddress string `json:"operator_address"`
            Status          string `json:"status"`
            Jailed          bool   `json:"jailed"`
            Tokens          string `json:"tokens"`
            Commission      struct {
                CommissionRates struct {
                    Rate          string `json:"rate"`
                    MaxRate       string `json:"max_rate"`
                    MaxChangeRate string `json:"max_change_rate"`
                } `json:"commission_rates"`
            } `json:"commission"`
        } `json:"validators"`
    }
    if err := json.Unmarshal(output, &result); err != nil {
        // If JSON parse fails, print raw output for diagnostics
        fmt.Println(string(output))
        return nil
    }
    if len(result.Validators) == 0 {
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
    }
    vals := make([]validatorDisplay, len(result.Validators))
    var wg sync.WaitGroup

    for i, v := range result.Validators {
        vals[i] = validatorDisplay{moniker: v.Description.Moniker, operatorAddr: v.OperatorAddress, cosmosAddr: v.OperatorAddress, jailed: v.Jailed}
        if vals[i].moniker == "" { vals[i].moniker = "unknown" }
        switch v.Status {
        case "BOND_STATUS_BONDED":
            vals[i].status, vals[i].statusOrder = "BONDED", 1
        case "BOND_STATUS_UNBONDING":
            vals[i].status, vals[i].statusOrder = "UNBONDING", 2
        case "BOND_STATUS_UNBONDED":
            vals[i].status, vals[i].statusOrder = "UNBONDED", 3
        default:
            vals[i].status, vals[i].statusOrder = v.Status, 4
        }
        if v.Tokens != "" && v.Tokens != "0" {
            if t, err := strconv.ParseFloat(v.Tokens, 64); err == nil { vals[i].tokensPC = t / 1e18 }
        }
        if v.Commission.CommissionRates.Rate != "" {
            if c, err := strconv.ParseFloat(v.Commission.CommissionRates.Rate, 64); err == nil { vals[i].commissionPct = c * 100 }
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
        isMyValidator := myValidatorAddr != "" && v.operatorAddr == myValidatorAddr
        if isMyValidator {
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
            fmt.Sprintf("%.1f", v.tokensPC),
            fmt.Sprintf("%.0f%%", v.commissionPct),
            v.commissionRwd,
            v.outstandingRwd,
            truncateAddress(v.evmAddress, 16),
        }

        // Apply green highlighting to the entire row if it's my validator
        if isMyValidator {
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
