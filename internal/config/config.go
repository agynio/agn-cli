package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/agynio/agn-cli/internal/tokencounting"
	"gopkg.in/yaml.v3"
)

const configEnvVar = "AGN_CONFIG_PATH"

type Config struct {
	LLM           LLMConfig           `yaml:"llm"`
	SystemPrompt  string              `yaml:"system_prompt"`
	Loop          LoopConfig          `yaml:"loop"`
	Summarization SummarizationConfig `yaml:"summarization"`
	TokenCounting TokenCountingConfig `yaml:"token_counting"`
	Tools         ToolsConfig         `yaml:"tools"`
	MCP           MCPConfig           `yaml:"mcp"`
}

type LLMConfig struct {
	Endpoint string     `yaml:"endpoint"`
	Auth     AuthConfig `yaml:"auth"`
	Model    string     `yaml:"model"`
}

type SummarizationConfig struct {
	LLM        *LLMConfig `yaml:"llm"`
	KeepTokens int        `yaml:"keep_tokens"`
	MaxTokens  int        `yaml:"max_tokens"`
}

type TokenCountingConfig struct {
	Address string `yaml:"address"`
	Timeout int    `yaml:"timeout"`
	Model   string `yaml:"model"`
}

type ToolsConfig struct {
	Shell ShellToolConfig `yaml:"shell"`
}

type ShellToolConfig struct {
	Enabled        *bool `yaml:"enabled"`
	Timeout        int   `yaml:"timeout"`
	IdleTimeout    int   `yaml:"idle_timeout"`
	MaxTimeout     int   `yaml:"max_timeout"`
	MaxIdleTimeout int   `yaml:"max_idle_timeout"`
	MaxOutput      int   `yaml:"max_output"`
}

type LoopConfig struct {
	MaxSteps *int `yaml:"max_steps"`
}

type AuthConfig struct {
	APIKey    string `yaml:"api_key"`
	APIKeyEnv string `yaml:"api_key_env"`
}

type MCPConfig struct {
	Servers map[string]MCPServerConfig `yaml:"servers"`
}

type MCPServerConfig struct {
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env"`
	URL     string            `yaml:"url"`
}

var mcpServerNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

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
	if err := c.LLM.Validate(); err != nil {
		return err
	}
	if err := c.Summarization.Validate(); err != nil {
		return err
	}
	if err := c.TokenCounting.Validate(); err != nil {
		return err
	}
	if err := c.Loop.Validate(); err != nil {
		return err
	}
	if err := c.Tools.Validate(); err != nil {
		return err
	}
	if err := c.MCP.Validate(); err != nil {
		return err
	}
	return nil
}

func (c LLMConfig) Validate() error {
	if strings.TrimSpace(c.Endpoint) == "" {
		return errors.New("llm.endpoint is required")
	}
	parsed, err := url.Parse(c.Endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("llm.endpoint is invalid: %q", c.Endpoint)
	}
	if strings.TrimSpace(c.Model) == "" {
		return errors.New("llm.model is required")
	}
	if err := c.Auth.Validate(); err != nil {
		return err
	}
	return nil
}

func (s SummarizationConfig) Validate() error {
	if s.LLM == nil {
		return nil
	}
	if err := s.LLM.Validate(); err != nil {
		return fmt.Errorf("summarization.%s", err)
	}
	return nil
}

func (t TokenCountingConfig) AddressValue() string {
	trimmed := strings.TrimSpace(t.Address)
	if trimmed == "" {
		return tokencounting.DefaultAddress
	}
	return trimmed
}

func (t TokenCountingConfig) TimeoutValue() time.Duration {
	if t.Timeout <= 0 {
		return tokencounting.DefaultTimeout
	}
	return time.Duration(t.Timeout) * time.Second
}

func (t TokenCountingConfig) Validate() error {
	trimmed := strings.TrimSpace(t.Address)
	if trimmed == "" {
		if t.Timeout < 0 {
			return errors.New("token_counting.timeout must be >= 0")
		}
		if strings.TrimSpace(t.Model) != "" {
			if _, err := tokencounting.ModelFromConfig(t.Model); err != nil {
				return fmt.Errorf("token_counting.model is invalid: %w", err)
			}
		}
		return nil
	}
	if strings.ContainsAny(trimmed, " \t\n\r") {
		return errors.New("token_counting.address must not contain whitespace")
	}
	if t.Timeout < 0 {
		return errors.New("token_counting.timeout must be >= 0")
	}
	if strings.TrimSpace(t.Model) != "" {
		if _, err := tokencounting.ModelFromConfig(t.Model); err != nil {
			return fmt.Errorf("token_counting.model is invalid: %w", err)
		}
	}
	return nil
}

func (l LoopConfig) Validate() error {
	if l.MaxSteps == nil {
		return nil
	}
	if *l.MaxSteps < 1 {
		return errors.New("loop.max_steps must be >= 1")
	}
	return nil
}

func (t ToolsConfig) Validate() error {
	return t.Shell.Validate()
}

func (s ShellToolConfig) EnabledValue() bool {
	if s.Enabled == nil {
		return true
	}
	return *s.Enabled
}

func (s ShellToolConfig) Validate() error {
	if s.Timeout < 0 {
		return errors.New("tools.shell.timeout must be >= 0")
	}
	if s.IdleTimeout < 0 {
		return errors.New("tools.shell.idle_timeout must be >= 0")
	}
	if s.MaxTimeout < 0 {
		return errors.New("tools.shell.max_timeout must be >= 0")
	}
	if s.MaxIdleTimeout < 0 {
		return errors.New("tools.shell.max_idle_timeout must be >= 0")
	}
	if s.MaxOutput < 0 {
		return errors.New("tools.shell.max_output must be >= 0")
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
	if strings.TrimSpace(a.APIKey) != "" {
		return strings.TrimSpace(a.APIKey), nil
	}
	value := strings.TrimSpace(os.Getenv(a.APIKeyEnv))
	if value == "" {
		return "", fmt.Errorf("environment variable %q is empty", a.APIKeyEnv)
	}
	return value, nil
}

func (c MCPConfig) Validate() error {
	if len(c.Servers) == 0 {
		return nil
	}
	for name, server := range c.Servers {
		if !mcpServerNamePattern.MatchString(name) {
			return fmt.Errorf("mcp.servers.%s name is invalid", name)
		}
		if err := server.Validate(); err != nil {
			return fmt.Errorf("mcp.servers.%s.%s", name, err)
		}
	}
	return nil
}

func (s MCPServerConfig) Validate() error {
	command := strings.TrimSpace(s.Command)
	url := strings.TrimSpace(s.URL)
	commandSet := command != ""
	urlSet := url != ""
	if commandSet == urlSet {
		return errors.New("exactly one of command or url is required")
	}
	if urlSet {
		if len(s.Args) > 0 {
			return errors.New("args are only valid with command")
		}
		if len(s.Env) > 0 {
			return errors.New("env is only valid with command")
		}
	}
	return nil
}
