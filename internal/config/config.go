package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const configEnvVar = "AGN_CONFIG_PATH"

type Config struct {
	LLM          LLMConfig `yaml:"llm"`
	SystemPrompt string    `yaml:"system_prompt"`
}

type LLMConfig struct {
	Endpoint string     `yaml:"endpoint"`
	Auth     AuthConfig `yaml:"auth"`
	Model    string     `yaml:"model"`
}

type AuthConfig struct {
	APIKey    string `yaml:"api_key"`
	APIKeyEnv string `yaml:"api_key_env"`
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".agyn", "agn", "config.yaml"), nil
}

func LoadDefault() (Config, error) {
	path := strings.TrimSpace(os.Getenv(configEnvVar))
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return Config{}, err
		}
	}
	return Load(path)
}

func Load(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		return Config{}, errors.New("config path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.LLM.Endpoint) == "" {
		return errors.New("llm.endpoint is required")
	}
	parsed, err := url.Parse(c.LLM.Endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("llm.endpoint is invalid: %q", c.LLM.Endpoint)
	}
	if strings.TrimSpace(c.LLM.Model) == "" {
		return errors.New("llm.model is required")
	}
	if err := c.LLM.Auth.Validate(); err != nil {
		return err
	}
	return nil
}

func (a AuthConfig) Validate() error {
	apiKey := strings.TrimSpace(a.APIKey)
	apiKeyEnv := strings.TrimSpace(a.APIKeyEnv)
	if apiKey == "" && apiKeyEnv == "" {
		return errors.New("llm.auth.api_key or llm.auth.api_key_env is required")
	}
	if apiKey != "" && apiKeyEnv != "" {
		return errors.New("only one of llm.auth.api_key or llm.auth.api_key_env can be set")
	}
	return nil
}

func (a AuthConfig) ResolveAPIKey() (string, error) {
	if err := a.Validate(); err != nil {
		return "", err
	}
	if strings.TrimSpace(a.APIKey) != "" {
		return strings.TrimSpace(a.APIKey), nil
	}
	value := strings.TrimSpace(os.Getenv(a.APIKeyEnv))
	if value == "" {
		return "", fmt.Errorf("environment variable %q is empty", a.APIKeyEnv)
	}
	return value, nil
}
