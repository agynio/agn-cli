package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func CredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".agyn", "credentials"), nil
}

func LoadCredentialsToken(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("credentials path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read credentials: %w", err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", errors.New("credentials file is empty")
	}
	return value, nil
}
