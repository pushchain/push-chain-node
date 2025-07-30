package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// QueryServer provides HTTP endpoints for querying configuration data
type QueryServer struct {
	client *UniversalClient
	logger *zap.Logger
	server *http.Server
}

// NewQueryServer creates a new QueryServer instance
func NewQueryServer(client *UniversalClient, logger *zap.Logger, port int) *QueryServer {
	qs := &QueryServer{
		client: client,
		logger: logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("/api/v1/chain-configs", qs.handleChainConfigs)
	mux.HandleFunc("/api/v1/token-configs", qs.handleTokenConfigs)
	mux.HandleFunc("/api/v1/token-configs-by-chain", qs.handleTokenConfigsByChain)
	mux.HandleFunc("/api/v1/token-config", qs.handleTokenConfig)

	qs.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	return qs
}

// Start starts the HTTP server
func (qs *QueryServer) Start() error {
	if qs.server == nil {
		return fmt.Errorf("query server is nil")
	}

	// Start server in goroutine
	go func() {
		err := qs.server.ListenAndServe()
		switch err {
		case nil:
			qs.logger.Info("Query server stopped normally")
		case http.ErrServerClosed:
			qs.logger.Info("Query server closed gracefully")
		default:
			qs.logger.Error("Query server error", zap.Error(err))
		}
		qs.logger.Info("Query server goroutine exiting")
	}()

	// Give the server time to start
	time.Sleep(100 * time.Millisecond)

	return nil
}

// Stop gracefully shuts down the HTTP server
func (qs *QueryServer) Stop() error {
	if qs.server != nil {
		return qs.server.Close()
	}
	return nil
}

// QueryResponse represents the standard query response format
type QueryResponse struct {
	Data        interface{} `json:"data"`
	LastFetched time.Time   `json:"last_fetched"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// handleChainConfigs handles GET /api/v1/chain-configs
func (qs *QueryServer) handleChainConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	configs := qs.client.GetAllChainConfigs()
	lastUpdate := qs.client.GetCacheLastUpdate()

	response := QueryResponse{
		Data:        configs,
		LastFetched: lastUpdate,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleTokenConfigs handles GET /api/v1/token-configs
func (qs *QueryServer) handleTokenConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	configs := qs.client.GetAllTokenConfigs()
	lastUpdate := qs.client.GetCacheLastUpdate()

	response := QueryResponse{
		Data:        configs,
		LastFetched: lastUpdate,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleTokenConfigsByChain handles GET /api/v1/token-configs-by-chain?chain=<chain>
func (qs *QueryServer) handleTokenConfigsByChain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	chain := r.URL.Query().Get("chain")
	if chain == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "chain parameter is required"})
		return
	}

	configs := qs.client.GetTokenConfigsByChain(chain)
	lastUpdate := qs.client.GetCacheLastUpdate()

	response := QueryResponse{
		Data:        configs,
		LastFetched: lastUpdate,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleTokenConfig handles GET /api/v1/token-config?chain=<chain>&address=<address>
func (qs *QueryServer) handleTokenConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	chain := r.URL.Query().Get("chain")
	address := r.URL.Query().Get("address")

	if chain == "" || address == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "chain and address parameters are required"})
		return
	}

	config := qs.client.GetTokenConfig(chain, address)
	if config == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("token config not found for chain %s and address %s", chain, address)})
		return
	}

	lastUpdate := qs.client.GetCacheLastUpdate()

	response := QueryResponse{
		Data:        config,
		LastFetched: lastUpdate,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
