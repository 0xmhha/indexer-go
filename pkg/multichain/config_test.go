package multichain

import (
	"testing"
	"time"
)

func TestDefaultManagerConfig(t *testing.T) {
	config := DefaultManagerConfig()

	if config == nil {
		t.Fatal("expected config to not be nil")
	}

	if config.Enabled {
		t.Error("default config should have Enabled=false")
	}

	if config.HealthCheckInterval != 30*time.Second {
		t.Errorf("expected HealthCheckInterval 30s, got %v", config.HealthCheckInterval)
	}

	if config.MaxUnhealthyDuration != 5*time.Minute {
		t.Errorf("expected MaxUnhealthyDuration 5m, got %v", config.MaxUnhealthyDuration)
	}

	if !config.AutoRestart {
		t.Error("default config should have AutoRestart=true")
	}

	if config.AutoRestartDelay != 30*time.Second {
		t.Errorf("expected AutoRestartDelay 30s, got %v", config.AutoRestartDelay)
	}
}

func TestDefaultChainConfig(t *testing.T) {
	config := DefaultChainConfig()

	if config == nil {
		t.Fatal("expected config to not be nil")
	}

	if config.AdapterType != "auto" {
		t.Errorf("expected AdapterType 'auto', got %s", config.AdapterType)
	}

	if !config.Enabled {
		t.Error("default chain config should be enabled")
	}

	if config.Workers != 4 {
		t.Errorf("expected Workers 4, got %d", config.Workers)
	}

	if config.BatchSize != 100 {
		t.Errorf("expected BatchSize 100, got %d", config.BatchSize)
	}

	if config.RPCTimeout != 30*time.Second {
		t.Errorf("expected RPCTimeout 30s, got %v", config.RPCTimeout)
	}
}

func TestChainConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  ChainConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: ChainConfig{
				ID:          "test-chain",
				Name:        "Test Chain",
				RPCEndpoint: "http://localhost:8545",
				ChainID:     1,
				AdapterType: "evm",
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			config: ChainConfig{
				Name:        "Test Chain",
				RPCEndpoint: "http://localhost:8545",
				ChainID:     1,
			},
			wantErr: true,
			errMsg:  "id is required",
		},
		{
			name: "missing name",
			config: ChainConfig{
				ID:          "test-chain",
				RPCEndpoint: "http://localhost:8545",
				ChainID:     1,
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "missing rpc_endpoint",
			config: ChainConfig{
				ID:      "test-chain",
				Name:    "Test Chain",
				ChainID: 1,
			},
			wantErr: true,
			errMsg:  "rpc_endpoint is required",
		},
		{
			name: "missing chain_id",
			config: ChainConfig{
				ID:          "test-chain",
				Name:        "Test Chain",
				RPCEndpoint: "http://localhost:8545",
			},
			wantErr: true,
			errMsg:  "chain_id is required",
		},
		{
			name: "invalid adapter type",
			config: ChainConfig{
				ID:          "test-chain",
				Name:        "Test Chain",
				RPCEndpoint: "http://localhost:8545",
				ChainID:     1,
				AdapterType: "invalid",
			},
			wantErr: true,
			errMsg:  "invalid adapter_type",
		},
		{
			name: "empty adapter type becomes auto",
			config: ChainConfig{
				ID:          "test-chain",
				Name:        "Test Chain",
				RPCEndpoint: "http://localhost:8545",
				ChainID:     1,
				AdapterType: "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" {
				if !containsString(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			}
		})
	}
}

func TestChainConfigValidateSetsDefaults(t *testing.T) {
	config := ChainConfig{
		ID:          "test-chain",
		Name:        "Test Chain",
		RPCEndpoint: "http://localhost:8545",
		ChainID:     1,
	}

	if err := config.Validate(); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	if config.AdapterType != "auto" {
		t.Errorf("expected AdapterType 'auto', got %s", config.AdapterType)
	}

	if config.Workers != 4 {
		t.Errorf("expected Workers 4, got %d", config.Workers)
	}

	if config.BatchSize != 100 {
		t.Errorf("expected BatchSize 100, got %d", config.BatchSize)
	}

	if config.RPCTimeout != 30*time.Second {
		t.Errorf("expected RPCTimeout 30s, got %v", config.RPCTimeout)
	}
}

func TestManagerConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  ManagerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "disabled config skips validation",
			config: ManagerConfig{
				Enabled: false,
				Chains:  []ChainConfig{}, // Empty but OK since disabled
			},
			wantErr: false,
		},
		{
			name: "enabled with no chains",
			config: ManagerConfig{
				Enabled: true,
				Chains:  []ChainConfig{},
			},
			wantErr: true,
			errMsg:  "no chains configured",
		},
		{
			name: "enabled with valid chain",
			config: ManagerConfig{
				Enabled: true,
				Chains: []ChainConfig{
					{
						ID:          "test-chain",
						Name:        "Test Chain",
						RPCEndpoint: "http://localhost:8545",
						ChainID:     1,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "enabled with duplicate chain IDs",
			config: ManagerConfig{
				Enabled: true,
				Chains: []ChainConfig{
					{
						ID:          "same-id",
						Name:        "Chain 1",
						RPCEndpoint: "http://localhost:8545",
						ChainID:     1,
					},
					{
						ID:          "same-id",
						Name:        "Chain 2",
						RPCEndpoint: "http://localhost:8546",
						ChainID:     2,
					},
				},
			},
			wantErr: true,
			errMsg:  "duplicate chain ID",
		},
		{
			name: "enabled with invalid chain config",
			config: ManagerConfig{
				Enabled: true,
				Chains: []ChainConfig{
					{
						ID:          "", // Invalid - missing ID
						Name:        "Test Chain",
						RPCEndpoint: "http://localhost:8545",
						ChainID:     1,
					},
				},
			},
			wantErr: true,
			errMsg:  "id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" {
				if !containsString(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			}
		})
	}
}

func TestManagerConfigValidateSetsDefaults(t *testing.T) {
	config := ManagerConfig{
		Enabled: true,
		Chains: []ChainConfig{
			{
				ID:          "test-chain",
				Name:        "Test Chain",
				RPCEndpoint: "http://localhost:8545",
				ChainID:     1,
			},
		},
		HealthCheckInterval:  0,
		MaxUnhealthyDuration: 0,
		AutoRestartDelay:     0,
	}

	if err := config.Validate(); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	if config.HealthCheckInterval != 30*time.Second {
		t.Errorf("expected HealthCheckInterval 30s, got %v", config.HealthCheckInterval)
	}

	if config.MaxUnhealthyDuration != 5*time.Minute {
		t.Errorf("expected MaxUnhealthyDuration 5m, got %v", config.MaxUnhealthyDuration)
	}

	if config.AutoRestartDelay != 30*time.Second {
		t.Errorf("expected AutoRestartDelay 30s, got %v", config.AutoRestartDelay)
	}
}

func TestManagerConfigGetEnabledChains(t *testing.T) {
	config := ManagerConfig{
		Chains: []ChainConfig{
			{ID: "chain-1", Enabled: true},
			{ID: "chain-2", Enabled: false},
			{ID: "chain-3", Enabled: true},
		},
	}

	enabled := config.GetEnabledChains()

	if len(enabled) != 2 {
		t.Errorf("expected 2 enabled chains, got %d", len(enabled))
	}

	// Verify the correct chains are returned
	ids := make(map[string]bool)
	for _, c := range enabled {
		ids[c.ID] = true
	}

	if !ids["chain-1"] || !ids["chain-3"] {
		t.Error("expected chain-1 and chain-3 to be enabled")
	}
}

func TestManagerConfigGetChainByID(t *testing.T) {
	config := ManagerConfig{
		Chains: []ChainConfig{
			{ID: "chain-1", Name: "Chain 1"},
			{ID: "chain-2", Name: "Chain 2"},
		},
	}

	// Find existing
	chain := config.GetChainByID("chain-1")
	if chain == nil {
		t.Fatal("expected to find chain-1")
	}
	if chain.Name != "Chain 1" {
		t.Errorf("expected name 'Chain 1', got %s", chain.Name)
	}

	// Find non-existing
	notFound := config.GetChainByID("nonexistent")
	if notFound != nil {
		t.Error("expected nil for non-existing chain")
	}
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
