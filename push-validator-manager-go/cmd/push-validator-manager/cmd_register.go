package main

import (
    "bufio"
    "context"
    "fmt"
    "math/big"
    "os"
    "strings"
    "time"

    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/config"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/node"
    "github.com/pushchain/push-chain-node/push-validator-manager-go/internal/validator"
)

// handleRegisterValidator is a compatibility wrapper that pulls
// defaults from env and invokes runRegisterValidator.
func handleRegisterValidator(cfg config.Config) {
    moniker := getenvDefault("MONIKER", "push-validator")
    keyName := getenvDefault("KEY_NAME", "validator-key")
    amount := getenvDefault("STAKE_AMOUNT", "1500000000000000000")
    runRegisterValidator(cfg, moniker, keyName, amount)
}

// runRegisterValidator performs the end-to-end registration flow:
// - verify node is not catching up
// - ensure key exists
// - wait for funding if necessary
// - submit create-validator transaction
// It prints text or JSON depending on --output.
func runRegisterValidator(cfg config.Config, moniker, keyName, amount string) {
    local := strings.TrimRight(cfg.RPCLocal, "/")
    if local == "" { local = "http://127.0.0.1:26657" }
    remoteHTTP := "https://" + strings.TrimSuffix(cfg.GenesisDomain, "/") + ":443"
    cliLocal := node.New(local)
    cliRemote := node.New(remoteHTTP)
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()
    stLocal, err1 := cliLocal.Status(ctx)
    _, err2 := cliRemote.RemoteStatus(ctx, remoteHTTP)
    if err1 == nil && err2 == nil {
        if stLocal.CatchingUp {
            if flagOutput == "json" { getPrinter().JSON(map[string]any{"ok": false, "error": "node is still syncing"}) } else { fmt.Println("node is still syncing. Run 'push-validator-manager sync' first") }
            return
        }
    }
    v := validator.NewWith(validator.Options{BinPath: findPchaind(), HomeDir: cfg.HomeDir, ChainID: cfg.ChainID, Keyring: cfg.KeyringBackend, GenesisDomain: cfg.GenesisDomain, Denom: cfg.Denom})
    ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Second)
    defer cancel2()
    addr, err := v.EnsureKey(ctx2, keyName)
    if err != nil { if flagOutput == "json" { getPrinter().JSON(map[string]any{"ok": false, "error": err.Error()}) } else { fmt.Printf("key error: %v\n", err) }; return }
    evmAddr, err := v.GetEVMAddress(ctx2, addr)
    if err != nil { evmAddr = "" }
    const requiredBalance = "1600000000000000000"
    const stakeAmount = "1500000000000000000"
    maxRetries := 10
    for tries := 0; tries < maxRetries; {
        balCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        bal, err := v.Balance(balCtx, addr); cancel()
        if err != nil {
            fmt.Printf("⚠️ Balance check failed: %v\n", err)
            tries++; time.Sleep(2*time.Second); continue
        }
        balInt := new(big.Int); balInt.SetString(bal, 10)
        reqInt := new(big.Int); reqInt.SetString(requiredBalance, 10)
        if balInt.Cmp(reqInt) >= 0 { fmt.Println("✅ Sufficient balance"); break }
        pcAmount := "0.000000"
        if bal != "0" { balFloat, _ := new(big.Float).SetString(bal); divisor := new(big.Float).SetFloat64(1e18); result := new(big.Float).Quo(balFloat, divisor); pcAmount = fmt.Sprintf("%.6f", result) }
        fmt.Printf("Balance: %s PC (need 1.6 PC)\n", pcAmount)
        fmt.Printf("Faucet: https://faucet.push.org | EVM: %s\n", evmAddr)
        fmt.Print("Press ENTER after funding...")
        reader := bufio.NewReader(os.Stdin); _, _ = reader.ReadString('\n')
    }
    stake := amount
    if stake == "" { stake = stakeAmount }
    txHash, err := v.Register(ctx2, validator.RegisterArgs{Moniker: moniker, Amount: stake, KeyName: keyName, CommissionRate: "0.10", MinSelfDelegation: "1"})
    if err != nil { if flagOutput == "json" { getPrinter().JSON(map[string]any{"ok": false, "error": err.Error()}) } else { fmt.Printf("register error: %v\n", err) }; return }
    if flagOutput == "json" { getPrinter().JSON(map[string]any{"ok": true, "txhash": txHash, "moniker": moniker, "key_name": keyName}) } else { fmt.Printf("validator registered. txhash: %s\n", txHash) }
    return
}
