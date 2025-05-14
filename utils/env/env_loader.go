// Package env provides utilities for loading environment variables from .env files
package env

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

var (
	// IsLoaded tracks whether the .env file has been loaded
	IsLoaded bool
)

// LoadEnv loads environment variables from .env file
// It's safe to call multiple times - will only load once
// It searches for .env files in the current directory and parent directories
func LoadEnv() error {
	if IsLoaded {
		return nil
	}

	// First try to load from current directory
	err := godotenv.Load()
	if err == nil {
		fmt.Println("Successfully loaded .env file from current directory")
		IsLoaded = true
		return nil
	}

	// If not found, try to search in parent directories
	currentDir, err := os.Getwd()
	if err != nil {
		fmt.Printf("Warning: Could not get current directory: %v\n", err)
	} else {
		// Try up to 5 parent directories
		for i := 0; i < 5; i++ {
			parentDir := filepath.Dir(currentDir)
			if parentDir == currentDir {
				// We've reached the root directory
				break
			}
			currentDir = parentDir

			envPath := filepath.Join(currentDir, ".env")
			if _, err := os.Stat(envPath); err == nil {
				// Found .env file in a parent directory
				err = godotenv.Load(envPath)
				if err == nil {
					fmt.Printf("Successfully loaded .env file from %s\n", envPath)
					IsLoaded = true
					return nil
				}
			}
		}
	}

	// If still not found, create a temporary one with default values
	tempEnvPath := ".env.temp"

	// Create a default .env file with Sepolia config
	defaultEnv := []byte("# Temporary .env file created for testing\n" +
		"USVL_CHAIN_RPC_11155111=https://ethereum-sepolia.publicnode.com\n")

	if writeErr := os.WriteFile(tempEnvPath, defaultEnv, 0644); writeErr != nil {
		fmt.Printf("Warning: Could not create temporary .env file: %v\n", writeErr)
	} else {
		defer os.Remove(tempEnvPath) // Clean up temp file after loading
		err = godotenv.Load(tempEnvPath)
		if err == nil {
			fmt.Println("Using temporary .env file with default values")
			IsLoaded = true
			return nil
		}
	}

	// Continue even if .env doesn't exist or can't be loaded
	fmt.Printf("Warning: Error loading .env file: %v\n", err)
	return err
}

// LoadEnvWithPath loads environment variables from a specific .env file path
func LoadEnvWithPath(filePath string) error {
	err := godotenv.Load(filePath)
	if err != nil {
		fmt.Printf("Warning: Error loading .env file at %s: %v\n", filePath, err)
		return err
	}

	fmt.Printf("Successfully loaded .env file from %s\n", filePath)
	IsLoaded = true
	return nil
}

// GetRpcUrlOverride gets a custom RPC URL from environment variables
// Format: "USVL_CHAIN_RPC_CHAINID" (e.g., USVL_CHAIN_RPC_11155111 for Sepolia)
func GetRpcUrlOverride(chainID string) (string, bool) {
	// Ensure environment variables are loaded
	LoadEnv()

	// Create environment variable name from chain ID
	envVarName := fmt.Sprintf("USVL_CHAIN_RPC_%s", strings.ToUpper(strings.Replace(chainID, "-", "_", -1)))

	// Check if the environment variable exists
	customRPC := os.Getenv(envVarName)
	if customRPC != "" {
		return customRPC, true
	}

	return "", false
}

// SetChainRpcEnvVar sets an RPC URL override for a chain
// This is useful for tests that need to set environment variables dynamically
func SetChainRpcEnvVar(chainID, rpcURL string) {
	envVarName := fmt.Sprintf("USVL_CHAIN_RPC_%s", strings.ToUpper(strings.Replace(chainID, "-", "_", -1)))
	os.Setenv(envVarName, rpcURL)
}
