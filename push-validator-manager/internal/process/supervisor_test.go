package process

import (
    "net"
    "testing"
    "time"
)

func TestIsRPCListening(t *testing.T) {
    ln, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil { t.Skipf("skipping: cannot bind due to sandbox: %v", err) }
    defer ln.Close()
    addr := ln.Addr().String()
    if !IsRPCListening(addr, 200*time.Millisecond) { t.Fatalf("expected listening true for %s", addr) }
    ln.Close()
    if IsRPCListening(addr, 200*time.Millisecond) { t.Fatalf("expected listening false after close for %s", addr) }
}
