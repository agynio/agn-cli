package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadCredentialsToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	require.NoError(t, os.WriteFile(path, []byte("token-value\n"), 0o600))

	value, err := LoadCredentialsToken(path)
	require.NoError(t, err)
	require.Equal(t, "token-value", value)
}
