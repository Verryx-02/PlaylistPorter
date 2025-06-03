package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds all application configuration
type Config struct {
	SPT  SPTConfig  `yaml:"spt"`
	TUBO TUBOConfig `yaml:"tubo"`
}

// SPTConfig holds SPT-specific configuration
type SPTConfig struct {
	ClientID     string   `yaml:"client_id"`
	ClientSecret string   `yaml:"client_secret"`
	RedirectURI  string   `yaml:"redirect_uri"`
	Scopes       []string `yaml:"scopes"`
}

// TUBOConfig holds TUBO Music-specific configuration
type TUBOConfig struct {
	ClientID     string   `yaml:"client_id"`
	ClientSecret string   `yaml:"client_secret"`
	RedirectURI  string   `yaml:"redirect_uri"`
	Scopes       []string `yaml:"scopes"`
}

// Load reads configuration from file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// validate checks if configuration is valid
func (c *Config) validate() error {
	if c.SPT.ClientID == "" {
		return fmt.Errorf("spt.client_id is required")
	}
	if c.SPT.ClientSecret == "" {
		return fmt.Errorf("spt.client_secret is required")
	}
	if c.TUBO.ClientID == "" {
		return fmt.Errorf("tubo.client_id is required")
	}
	if c.TUBO.ClientSecret == "" {
		return fmt.Errorf("tubo.client_secret is required")
	}
	return nil
}
