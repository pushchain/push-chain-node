package validator

import (
    "context"
    "os"
    "path/filepath"
    "runtime"
    "testing"
)

// Creates a fake pchaind executable that responds to the minimal subset of commands
// used by the validator service.
func makeFakePchaind(t *testing.T) string {
    dir := t.TempDir()
    bin := filepath.Join(dir, "pchaind")
    script := "#!/usr/bin/env sh\n" +
        "cmd=\"$1\"; shift\n" +
        "if [ \"$cmd\" = \"tendermint\" ]; then sub=\"$1\"; shift; if [ \"$sub\" = \"show-validator\" ]; then echo '{\"type\":\"tendermint/PubKeyEd25519\",\"key\":\"PUBKEYBASE64\"}'; exit 0; fi; fi\n" +
        "if [ \"$cmd\" = \"keys\" ]; then sub=\"$1\"; shift; if [ \"$sub\" = \"show\" ]; then echo 'push1addrxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx'; exit 0; fi; if [ \"$sub\" = \"add\" ]; then exit 0; fi; fi\n" +
        "if [ \"$cmd\" = \"query\" ]; then mod=\"$1\"; shift; if [ \"$mod\" = \"bank\" ]; then echo '{\"balances\":[{\"denom\":\"upc\",\"amount\":\"999\"}]}' ; exit 0; fi; if [ \"$mod\" = \"staking\" ]; then echo '{\"validators\":[]}' ; exit 0; fi; fi\n" +
        "if [ \"$cmd\" = \"tx\" ]; then mod=\"$1\"; shift; if [ \"$mod\" = \"staking\" ]; then echo 'txhash: 0xABCD'; exit 0; fi; fi\n" +
        "echo 'unknown'; exit 1\n"
    if err := os.WriteFile(bin, []byte(script), 0o755); err != nil { t.Fatal(err) }
    if runtime.GOOS == "windows" { t.Skip("windows not supported in this test") }
    return bin
}

func TestValidator_RegisterHappyPath(t *testing.T) {
    bin := makeFakePchaind(t)
    home := t.TempDir()
    s := NewWith(Options{
        BinPath: bin,
        HomeDir: home,
        ChainID: "push_42101-1",
        Keyring: "test",
        GenesisDomain: "rpc-testnet-donut-node1.push.org",
        Denom: "upc",
    })
    ctx := context.Background()
    // EnsureKey should return the fake address
    addr, err := s.EnsureKey(ctx, "validator-key")
    if err != nil { t.Fatal(err) }
    if addr == "" { t.Fatal("empty addr") }
    // IsValidator should be false (no validators in fake output)
    ok, err := s.IsValidator(ctx, addr)
    if err != nil { t.Fatal(err) }
    if ok { t.Fatal("expected not a validator") }
    // Balance should parse
    bal, err := s.Balance(ctx, addr)
    if err != nil { t.Fatal(err) }
    if bal != "999" { t.Fatalf("balance got %s", bal) }
    // Register should return txhash
    tx, err := s.Register(ctx, RegisterArgs{Moniker: "m", Amount: "1500000000000000000", KeyName: "validator-key"})
    if err != nil { t.Fatal(err) }
    if tx == "" { t.Fatal("empty txhash") }
}

