package loop

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agynio/agn-cli/internal/message"
)

func TestRouteToTools(t *testing.T) {
	agent := &Agent{maxRestrictAttempts: 1}
	state := &State{PendingToolCalls: []message.ToolCall{{ID: "1", Name: "tool"}}}

	next, err := agent.route(context.Background(), state)
	require.NoError(t, err)
	require.Equal(t, StageCallTools, next)
}

func TestRouteRestrictOutput(t *testing.T) {
	agent := &Agent{maxRestrictAttempts: 2}
	state := &State{RestrictOutput: true}

	next, err := agent.route(context.Background(), state)
	require.NoError(t, err)
	require.Equal(t, StageCallModel, next)
	require.True(t, state.ForceToolCall)
	require.Equal(t, 1, state.RestrictAttempts)
}

func TestRouteSave(t *testing.T) {
	agent := &Agent{maxRestrictAttempts: 1}
	state := &State{}

	next, err := agent.route(context.Background(), state)
	require.NoError(t, err)
	require.Equal(t, StageSave, next)
}

func TestRouteRestrictExceeds(t *testing.T) {
	agent := &Agent{maxRestrictAttempts: 1}
	state := &State{RestrictOutput: true, RestrictAttempts: 1}

	_, err := agent.route(context.Background(), state)
	require.Error(t, err)
}
