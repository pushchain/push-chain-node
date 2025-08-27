package config

// GetAuthZMessageTypeConfig returns configuration for authz message types  
// This returns the configuration without directly importing authz to avoid circular imports
func GetAuthZMessageTypeConfig(cfg *Config) (string, []string, error) {
	// Validate configuration first
	if err := cfg.ApplyMessageTypeConfiguration(); err != nil {
		return "", nil, err
	}

	category := cfg.GetEffectiveMessageTypeCategory()
	return category, cfg.CustomMessageTypes, nil
}