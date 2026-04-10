package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agynio/agn-cli/internal/config"
	"github.com/agynio/agn-cli/internal/loop"
)

func TestResolveMaxStepsDefault(t *testing.T) {
	maxSteps := resolveMaxSteps(config.Config{})
	require.Equal(t, loop.DefaultMaxSteps, maxSteps)
}

func TestResolveMaxStepsConfig(t *testing.T) {
	value := 321
	cfg := config.Config{Loop: config.LoopConfig{MaxSteps: &value}}
	maxSteps := resolveMaxSteps(cfg)
	require.Equal(t, value, maxSteps)
}
