package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/constant"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

// Output formats
const (
	OutputFormatYAML = "yaml"
	OutputFormatJSON = "json"
)

// ChainConfigOutput represents the output format for chain configs
type ChainConfigOutput struct {
	Config      *uregistrytypes.ChainConfig   `yaml:"config,omitempty" json:"config,omitempty"`
	Configs     []*uregistrytypes.ChainConfig `yaml:"configs,omitempty" json:"configs,omitempty"`
	LastFetched time.Time                     `yaml:"last_fetched" json:"last_fetched"`
}

// TokenConfigOutput represents the output format for token configs
type TokenConfigOutput struct {
	Config      *uregistrytypes.TokenConfig   `yaml:"config,omitempty" json:"config,omitempty"`
	Configs     []*uregistrytypes.TokenConfig `yaml:"configs,omitempty" json:"configs,omitempty"`
	LastFetched time.Time                     `yaml:"last_fetched" json:"last_fetched"`
}

// QueryResponse represents the standard query response format from HTTP API
type QueryResponse struct {
	Data        json.RawMessage `json:"data"`
	LastFetched time.Time       `json:"last_fetched"`
}

// ErrorResponse represents an error response from HTTP API
type ErrorResponse struct {
	Error string `json:"error"`
}

func queryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "query",
		Aliases: []string{"q"},
		Short:   "Querying commands",
	}

	cmd.AddCommand(uregistryCmd())
	return cmd
}

func uregistryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uregistry",
		Short: "Querying commands for the uregistry module",
	}

	cmd.AddCommand(
		allChainConfigsCmd(),
		allTokenConfigsCmd(),
		tokenConfigsByChainCmd(),
		tokenConfigCmd(),
	)

	return cmd
}

func allChainConfigsCmd() *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "all-chain-configs",
		Short: "Query all chain configurations",
		RunE: func(cmd *cobra.Command, args []string) error {
			port, err := getQueryServerPort()
			if err != nil {
				return err
			}

			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/v1/chain-configs", port))
			if err != nil {
				return fmt.Errorf("failed to query chain configs: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				var errResp ErrorResponse
				if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
					return fmt.Errorf("server returned status %d", resp.StatusCode)
				}
				return fmt.Errorf("server error: %s", errResp.Error)
			}

			var queryResp QueryResponse
			if err := json.NewDecoder(resp.Body).Decode(&queryResp); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			var configs []*uregistrytypes.ChainConfig
			if err := json.Unmarshal(queryResp.Data, &configs); err != nil {
				return fmt.Errorf("failed to unmarshal chain configs: %w", err)
			}

			output := ChainConfigOutput{
				Configs:     configs,
				LastFetched: queryResp.LastFetched,
			}

			return printOutput(output, outputFormat)
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", OutputFormatYAML, "Output format (yaml|json)")
	return cmd
}

func allTokenConfigsCmd() *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "all-token-configs",
		Short: "Query all token configurations",
		RunE: func(cmd *cobra.Command, args []string) error {
			port, err := getQueryServerPort()
			if err != nil {
				return err
			}

			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/v1/token-configs", port))
			if err != nil {
				return fmt.Errorf("failed to query token configs: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				var errResp ErrorResponse
				if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
					return fmt.Errorf("server returned status %d", resp.StatusCode)
				}
				return fmt.Errorf("server error: %s", errResp.Error)
			}

			var queryResp QueryResponse
			if err := json.NewDecoder(resp.Body).Decode(&queryResp); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			var configs []*uregistrytypes.TokenConfig
			if err := json.Unmarshal(queryResp.Data, &configs); err != nil {
				return fmt.Errorf("failed to unmarshal token configs: %w", err)
			}

			output := TokenConfigOutput{
				Configs:     configs,
				LastFetched: queryResp.LastFetched,
			}

			return printOutput(output, outputFormat)
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", OutputFormatYAML, "Output format (yaml|json)")
	return cmd
}

func tokenConfigsByChainCmd() *cobra.Command {
	var (
		chain        string
		outputFormat string
	)

	cmd := &cobra.Command{
		Use:   "token-configs-by-chain",
		Short: "Query all token configurations for a specific chain",
		RunE: func(cmd *cobra.Command, args []string) error {
			if chain == "" {
				return fmt.Errorf("chain is required")
			}

			port, err := getQueryServerPort()
			if err != nil {
				return err
			}

			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/v1/token-configs-by-chain?chain=%s", port, chain))
			if err != nil {
				return fmt.Errorf("failed to query token configs by chain: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				var errResp ErrorResponse
				if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
					return fmt.Errorf("server returned status %d", resp.StatusCode)
				}
				return fmt.Errorf("server error: %s", errResp.Error)
			}

			var queryResp QueryResponse
			if err := json.NewDecoder(resp.Body).Decode(&queryResp); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			var configs []*uregistrytypes.TokenConfig
			if err := json.Unmarshal(queryResp.Data, &configs); err != nil {
				return fmt.Errorf("failed to unmarshal token configs: %w", err)
			}

			output := TokenConfigOutput{
				Configs:     configs,
				LastFetched: queryResp.LastFetched,
			}

			return printOutput(output, outputFormat)
		},
	}

	cmd.Flags().StringVar(&chain, "chain", "", "Chain ID (e.g., eip155:11155111)")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", OutputFormatYAML, "Output format (yaml|json)")
	cmd.MarkFlagRequired("chain")

	return cmd
}

func tokenConfigCmd() *cobra.Command {
	var (
		chain        string
		address      string
		outputFormat string
	)

	cmd := &cobra.Command{
		Use:   "token-config",
		Short: "Query a specific token configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			if chain == "" {
				return fmt.Errorf("chain is required")
			}
			if address == "" {
				return fmt.Errorf("address is required")
			}

			port, err := getQueryServerPort()
			if err != nil {
				return err
			}

			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/v1/token-config?chain=%s&address=%s", port, chain, address))
			if err != nil {
				return fmt.Errorf("failed to query token config: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusNotFound {
				var errResp ErrorResponse
				if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
					return fmt.Errorf("token config not found for chain %s and address %s", chain, address)
				}
				return fmt.Errorf(errResp.Error)
			}

			if resp.StatusCode != http.StatusOK {
				var errResp ErrorResponse
				if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
					return fmt.Errorf("server returned status %d", resp.StatusCode)
				}
				return fmt.Errorf("server error: %s", errResp.Error)
			}

			var queryResp QueryResponse
			if err := json.NewDecoder(resp.Body).Decode(&queryResp); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			var config *uregistrytypes.TokenConfig
			if err := json.Unmarshal(queryResp.Data, &config); err != nil {
				return fmt.Errorf("failed to unmarshal token config: %w", err)
			}

			output := TokenConfigOutput{
				Config:      config,
				LastFetched: queryResp.LastFetched,
			}

			return printOutput(output, outputFormat)
		},
	}

	cmd.Flags().StringVar(&chain, "chain", "", "Chain ID (e.g., eip155:11155111)")
	cmd.Flags().StringVar(&address, "address", "", "Token address")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", OutputFormatYAML, "Output format (yaml|json)")
	cmd.MarkFlagRequired("chain")
	cmd.MarkFlagRequired("address")

	return cmd
}

// getQueryServerPort loads the config to get the query server port
func getQueryServerPort() (int, error) {
	// Load config
	loadedCfg, err := config.Load(constant.DefaultNodeHome)
	if err != nil {
		return 0, fmt.Errorf("failed to load config: %w", err)
	}

	return loadedCfg.QueryServerPort, nil
}

// printOutput prints the output in the specified format
func printOutput(data interface{}, format string) error {
	switch format {
	case OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(data)
	case OutputFormatYAML:
		encoder := yaml.NewEncoder(os.Stdout)
		return encoder.Encode(data)
	default:
		return fmt.Errorf("unsupported output format: %s", format)
	}
}
