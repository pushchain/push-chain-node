package main

import (
    "context"
    "encoding/json"
    "fmt"
    "os/exec"
    "sort"
    "strconv"
    "time"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/config"
    ui "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/ui"
)

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
    type validatorDisplay struct {
        moniker       string
        status        string
        statusOrder   int
        tokensPC      float64
        commissionPct float64
    }
    vals := make([]validatorDisplay, 0, len(result.Validators))
    for _, v := range result.Validators {
        vd := validatorDisplay{moniker: v.Description.Moniker}
        if vd.moniker == "" { vd.moniker = "unknown" }
        switch v.Status {
        case "BOND_STATUS_BONDED":
            vd.status, vd.statusOrder = "BONDED", 1
        case "BOND_STATUS_UNBONDING":
            vd.status, vd.statusOrder = "UNBONDING", 2
        case "BOND_STATUS_UNBONDED":
            vd.status, vd.statusOrder = "UNBONDED", 3
        default:
            vd.status, vd.statusOrder = v.Status, 4
        }
        if v.Tokens != "" && v.Tokens != "0" {
            if t, err := strconv.ParseFloat(v.Tokens, 64); err == nil { vd.tokensPC = t / 1e18 }
        }
        if v.Commission.CommissionRates.Rate != "" {
            if c, err := strconv.ParseFloat(v.Commission.CommissionRates.Rate, 64); err == nil { vd.commissionPct = c * 100 }
        }
        vals = append(vals, vd)
    }
    sort.Slice(vals, func(i, j int) bool {
        if vals[i].statusOrder != vals[j].statusOrder { return vals[i].statusOrder < vals[j].statusOrder }
        return vals[i].tokensPC > vals[j].tokensPC
    })
    c := ui.NewColorConfig()
    fmt.Println()
    fmt.Println(c.Header(" 👥 Active Push Chain Validators "))
    headers := []string{"VALIDATOR", "STATUS", "STAKE (PC)", "COMMISSION"}
    rows := make([][]string, 0, len(vals))
    for _, v := range vals {
        rows = append(rows, []string{
            truncate(v.moniker, 26),
            v.status,
            fmt.Sprintf("%.1f", v.tokensPC),
            fmt.Sprintf("%.0f%%", v.commissionPct),
        })
    }
    fmt.Print(ui.Table(c, headers, rows, nil))
    fmt.Printf("Total Validators: %d\n", len(vals))
    return nil
}

// truncate returns s truncated to max characters with ellipsis when needed.
func truncate(s string, max int) string {
    if len(s) <= max { return s }
    return s[:max-3] + "..."
}
