package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/agynio/agn-cli/internal/config"
	"github.com/agynio/agn-cli/internal/loop"
)

func newTestCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "agn"}
	cmd.Flags().Int("max-steps", loop.DefaultMaxSteps, "Maximum loop steps")
	return cmd
}

func TestResolveMaxStepsDefault(t *testing.T) {
	cmd := newTestCommand()
	t.Setenv(loopMaxStepsEnvVar, "")

	maxSteps, err := resolveMaxSteps(cmd, config.Config{})
	require.NoError(t, err)
	require.Equal(t, loop.DefaultMaxSteps, maxSteps)
}

func TestResolveMaxStepsConfig(t *testing.T) {
	cmd := newTestCommand()
	t.Setenv(loopMaxStepsEnvVar, "")
	value := 321
	cfg := config.Config{Loop: config.LoopConfig{MaxSteps: &value}}

	maxSteps, err := resolveMaxSteps(cmd, cfg)
	require.NoError(t, err)
	require.Equal(t, value, maxSteps)
}

func TestResolveMaxStepsEnvOverridesConfig(t *testing.T) {
	cmd := newTestCommand()
	t.Setenv(loopMaxStepsEnvVar, "456")
	value := 321
	cfg := config.Config{Loop: config.LoopConfig{MaxSteps: &value}}

	maxSteps, err := resolveMaxSteps(cmd, cfg)
	require.NoError(t, err)
	require.Equal(t, 456, maxSteps)
}

func TestResolveMaxStepsFlagOverridesEnv(t *testing.T) {
	cmd := newTestCommand()
	t.Setenv(loopMaxStepsEnvVar, "456")
	value := 321
	cfg := config.Config{Loop: config.LoopConfig{MaxSteps: &value}}
	require.NoError(t, cmd.Flags().Set("max-steps", "789"))

	maxSteps, err := resolveMaxSteps(cmd, cfg)
	require.NoError(t, err)
	require.Equal(t, 789, maxSteps)
}

func TestResolveMaxStepsEnvInvalid(t *testing.T) {
	cmd := newTestCommand()
	t.Setenv(loopMaxStepsEnvVar, "nope")

	_, err := resolveMaxSteps(cmd, config.Config{})
	require.Error(t, err)
	require.ErrorContains(t, err, "AGN_LOOP_MAX_STEPS must be an integer >= 1")
}

func TestResolveMaxStepsFlagInvalid(t *testing.T) {
	cmd := newTestCommand()
	require.NoError(t, cmd.Flags().Set("max-steps", "0"))

	_, err := resolveMaxSteps(cmd, config.Config{})
	require.Error(t, err)
	require.ErrorContains(t, err, "--max-steps must be >= 1")
}
