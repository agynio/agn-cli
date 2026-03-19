package loop

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewLoopValidation(t *testing.T) {
	_, err := NewLoop(nil, StageLoad, 1)
	require.Error(t, err)

	_, err = NewLoop(map[StageID]Stage{StageLoad: {}}, "", 1)
	require.Error(t, err)

	_, err = NewLoop(map[StageID]Stage{StageLoad: {}}, StageLoad, 0)
	require.Error(t, err)
}

func TestLoopRunPipeline(t *testing.T) {
	var visited []StageID
	stages := map[StageID]Stage{
		"start": {
			Reducer: func(ctx context.Context, state *State) error {
				visited = append(visited, "start")
				return nil
			},
			Next: "middle",
		},
		"middle": {
			Reducer: func(ctx context.Context, state *State) error {
				visited = append(visited, "middle")
				return nil
			},
			Next: StageDone,
		},
	}
	loop, err := NewLoop(stages, "start", 5)
	require.NoError(t, err)
	require.NoError(t, loop.Run(context.Background(), &State{}))
	require.Equal(t, []StageID{"start", "middle"}, visited)
}

func TestLoopRunUnknownStage(t *testing.T) {
	loop, err := NewLoop(map[StageID]Stage{StageLoad: {}}, "missing", 2)
	require.NoError(t, err)
	err = loop.Run(context.Background(), &State{})
	require.Error(t, err)
}

func TestLoopRunMaxSteps(t *testing.T) {
	loop, err := NewLoop(map[StageID]Stage{
		"loop": {Next: "loop"},
	}, "loop", 2)
	require.NoError(t, err)
	err = loop.Run(context.Background(), &State{})
	require.Error(t, err)
}

func TestLoopRunContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	loop, err := NewLoop(map[StageID]Stage{
		"start": {
			Reducer: func(ctx context.Context, state *State) error {
				<-ctx.Done()
				return ctx.Err()
			},
			Next: StageDone,
		},
	}, "start", 1)
	require.NoError(t, err)
	err = loop.Run(ctx, &State{})
	require.ErrorIs(t, err, context.Canceled)
}

func TestLoopRunRouterBranching(t *testing.T) {
	stages := map[StageID]Stage{
		"route": {
			Router: func(ctx context.Context, state *State) (StageID, error) {
				if state.ForceToolCall {
					return "tool", nil
				}
				return "save", nil
			},
		},
		"tool": {
			Reducer: func(ctx context.Context, state *State) error {
				state.LastAssistant = "tool"
				return nil
			},
			Next: StageDone,
		},
		"save": {
			Reducer: func(ctx context.Context, state *State) error {
				state.LastAssistant = "save"
				return nil
			},
			Next: StageDone,
		},
	}
	loop, err := NewLoop(stages, "route", 3)
	require.NoError(t, err)

	state := &State{ForceToolCall: true}
	require.NoError(t, loop.Run(context.Background(), state))
	require.Equal(t, "tool", state.LastAssistant)
}
