package utils

import (
	"fmt"
	"os"
	"strings"
)

// chain is a CAIP-2 chain identifier, e.g., "eip155:1"
func GetEnvRPCOverride(chain string) string {
	// Convert CAIP-2 to uppercase and replace special characters to make it ENV-safe
	// e.g., "eip155:1" -> "RPC_URL_EIP155_1"
	envKey := fmt.Sprintf("RPC_URL_%s", formatCAIP2ForEnv(chain))

	if val := os.Getenv(envKey); val != "" {
		return val
	}
	return ""
}

func formatCAIP2ForEnv(caip2 string) string {
	caip2 = strings.ToUpper(caip2)
	caip2 = strings.ReplaceAll(caip2, ":", "_")
	caip2 = strings.ReplaceAll(caip2, "-", "_")
	return caip2
}
