package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// handleHealth handles GET /health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// handleChainConfigs handles GET /api/v1/chain-configs
func (s *Server) handleChainConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	configs := s.client.GetAllChainConfigs()
	lastUpdate := s.client.GetCacheLastUpdate()

	response := QueryResponse{
		Data:        configs,
		LastFetched: lastUpdate,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleTokenConfigs handles GET /api/v1/token-configs
func (s *Server) handleTokenConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	configs := s.client.GetAllTokenConfigs()
	lastUpdate := s.client.GetCacheLastUpdate()

	response := QueryResponse{
		Data:        configs,
		LastFetched: lastUpdate,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleTokenConfigsByChain handles GET /api/v1/token-configs-by-chain?chain=<chain>
func (s *Server) handleTokenConfigsByChain(w http.ResponseWriter, r *http.Request) {
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

	configs := s.client.GetTokenConfigsByChain(chain)
	lastUpdate := s.client.GetCacheLastUpdate()

	response := QueryResponse{
		Data:        configs,
		LastFetched: lastUpdate,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleTokenConfig handles GET /api/v1/token-config?chain=<chain>&address=<address>
func (s *Server) handleTokenConfig(w http.ResponseWriter, r *http.Request) {
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

	config := s.client.GetTokenConfig(chain, address)
	if config == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("token config not found for chain %s and address %s", chain, address)})
		return
	}

	lastUpdate := s.client.GetCacheLastUpdate()

	response := QueryResponse{
		Data:        config,
		LastFetched: lastUpdate,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}