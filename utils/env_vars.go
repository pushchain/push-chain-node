package utils

import (
	"fmt"
	"os"
	"strings"
)

func GetEnvRPCOverride(chainId string, fallback string) string {
	envKey := fmt.Sprintf("RPC_URL_%s", chainId)
	envKey = strings.ReplaceAll(envKey, "-", "_")

	if val := os.Getenv(envKey); val != "" {
		return val
	}
	return fallback
}
