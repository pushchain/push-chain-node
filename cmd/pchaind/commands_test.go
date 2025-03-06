package main

import ("testing")

func TestInitCometBFTConfig(t *testing.T) {
	cfg := initCometBFTConfig()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	// Check that some default value is set. For example, the TCP address should be non-empty.
	if cfg.RPC.ListenAddress == "" {
		t.Error("expected RPC.ListenAddress to be non-empty")
	}
}

func TestTxCommand(t *testing.T) {
	cmd := txCommand()
	if cmd == nil {
		t.Fatal(("Expected non-nil command output"))
	}
}