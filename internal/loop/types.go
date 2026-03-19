package loop

import (
	"context"
	"errors"
	"fmt"
)

type StageID string

const (
	StageLoad      StageID = "load"
	StageSummarize StageID = "summarize"
	StageCallModel StageID = "call_model"
	StageRoute     StageID = "route"
	StageCallTools StageID = "call_tools"
	StageSave      StageID = "save"
	StageDone      StageID = "done"
)

type Reducer func(ctx context.Context, state *State) error

type Router func(ctx context.Context, state *State) (StageID, error)

type Stage struct {
	Reducer Reducer
	Router  Router
	Next    StageID
}

type Loop struct {
	stages   map[StageID]Stage
	start    StageID
	maxSteps int
}

func NewLoop(stages map[StageID]Stage, start StageID, maxSteps int) (*Loop, error) {
	if len(stages) == 0 {
		return nil, errors.New("stages are required")
	}
	if start == "" {
		return nil, errors.New("start stage is required")
	}
	if maxSteps <= 0 {
		return nil, errors.New("max steps must be positive")
	}
	return &Loop{stages: stages, start: start, maxSteps: maxSteps}, nil
}

func (l *Loop) Run(ctx context.Context, state *State) error {
	current := l.start
	for step := 0; step < l.maxSteps; step++ {
		stage, ok := l.stages[current]
		if !ok {
			return fmt.Errorf("unknown stage %q", current)
		}
		if stage.Reducer != nil {
			if err := stage.Reducer(ctx, state); err != nil {
				return err
			}
		}
		if stage.Router != nil {
			next, err := stage.Router(ctx, state)
			if err != nil {
				return err
			}
			current = next
		} else {
			current = stage.Next
		}
		if current == StageDone {
			return nil
		}
	}
	return errors.New("loop exceeded max steps")
}
