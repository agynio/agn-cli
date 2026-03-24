package loop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"github.com/agynio/agn-cli/internal/llm"
	"github.com/agynio/agn-cli/internal/mcp"
	"github.com/agynio/agn-cli/internal/message"
	"github.com/agynio/agn-cli/internal/state"
	"github.com/agynio/agn-cli/internal/summarize"
)

const (
	defaultMaxSteps            = 20
	defaultMaxRestrictAttempts = 2
)

type EventType string

const (
	EventModelDelta  EventType = "model_delta"
	EventTurnStarted EventType = "turn_started"
	EventTurnDone    EventType = "turn_done"
	EventItemStarted EventType = "item_started"
	EventItemDone    EventType = "item_done"
)

type Event struct {
	Type       EventType
	ThreadID   string
	TurnID     string
	ItemID     string
	Delta      string
	ToolName   string
	ToolCallID string
}

type EventSink func(Event)

type Input struct {
	ThreadID       string
	Prompt         message.HumanMessage
	RestrictOutput bool
	Stream         bool
	EventSink      EventSink
}

type Result struct {
	ThreadID string
	Response string
}

type State struct {
	Thread           state.Thread
	TurnID           string
	Input            *message.HumanMessage
	RestrictOutput   bool
	Stream           bool
	RestrictAttempts int
	ForceToolCall    bool
	Tools            []responses.ToolUnionParam
	PendingToolCalls []message.ToolCall
	LastAssistant    string
	EventSink        EventSink
}

type AgentConfig struct {
	Store               state.Store
	LLM                 *llm.Client
	Summarizer          *summarize.Summarizer
	MCP                 *mcp.Client
	SystemPrompt        string
	MaxSteps            int
	MaxRestrictAttempts int
}

type Agent struct {
	store               state.Store
	llm                 *llm.Client
	summarizer          *summarize.Summarizer
	mcp                 *mcp.Client
	systemPrompt        string
	maxRestrictAttempts int
	loop                *Loop
}

func NewAgent(cfg AgentConfig) (*Agent, error) {
	if cfg.Store == nil {
		return nil, errors.New("state store is required")
	}
	if cfg.LLM == nil {
		return nil, errors.New("llm client is required")
	}
	if cfg.Summarizer == nil {
		return nil, errors.New("summarizer is required")
	}
	maxSteps := cfg.MaxSteps
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
	}
	maxRestrict := cfg.MaxRestrictAttempts
	if maxRestrict <= 0 {
		maxRestrict = defaultMaxRestrictAttempts
	}

	agent := &Agent{
		store:               cfg.Store,
		llm:                 cfg.LLM,
		summarizer:          cfg.Summarizer,
		mcp:                 cfg.MCP,
		systemPrompt:        cfg.SystemPrompt,
		maxRestrictAttempts: maxRestrict,
	}

	stages := map[StageID]Stage{
		StageLoad:      {Reducer: agent.load, Next: StageSummarize},
		StageSummarize: {Reducer: agent.summarize, Next: StageCallModel},
		StageCallModel: {Reducer: agent.callModel, Next: StageRoute},
		StageRoute:     {Router: agent.route},
		StageCallTools: {Reducer: agent.callTools, Next: StageCallModel},
		StageSave:      {Reducer: agent.save, Next: StageDone},
	}
	loop, err := NewLoop(stages, StageLoad, maxSteps)
	if err != nil {
		return nil, err
	}
	agent.loop = loop
	return agent, nil
}

func (a *Agent) Run(ctx context.Context, input Input) (Result, error) {
	threadID := strings.TrimSpace(input.ThreadID)
	if threadID == "" {
		threadID = uuid.NewString()
	}
	if strings.TrimSpace(input.Prompt.Text) == "" {
		return Result{}, errors.New("prompt is required")
	}
	if input.Stream && input.EventSink == nil {
		return Result{}, errors.New("streaming requires an event sink")
	}
	turnID := uuid.NewString()
	if input.EventSink != nil {
		input.EventSink(Event{Type: EventTurnStarted, ThreadID: threadID, TurnID: turnID})
		defer input.EventSink(Event{Type: EventTurnDone, ThreadID: threadID, TurnID: turnID})
	}
	state := &State{
		Thread:         state.Thread{ID: threadID, Messages: []state.MessageRecord{}},
		TurnID:         turnID,
		Input:          &input.Prompt,
		RestrictOutput: input.RestrictOutput,
		Stream:         input.Stream,
		EventSink:      input.EventSink,
	}
	if err := a.loop.Run(ctx, state); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(state.LastAssistant) == "" {
		return Result{}, errors.New("no response generated")
	}
	return Result{ThreadID: threadID, Response: state.LastAssistant}, nil
}

func (a *Agent) load(ctx context.Context, state *State) error {
	thread, err := a.store.Load(ctx, state.Thread.ID)
	if err != nil {
		return err
	}
	state.Thread = thread
	if state.Input != nil {
		record, err := a.recordFromMessage("", *state.Input)
		if err != nil {
			return err
		}
		state.Thread.Messages = append(state.Thread.Messages, record)
	}
	if a.mcp != nil {
		tools, err := a.mcp.ListTools(ctx)
		if err != nil {
			return err
		}
		state.Tools, err = llm.ToolDefinitionsFromMCP(tools)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *Agent) summarize(ctx context.Context, state *State) error {
	updated, err := a.summarizer.Summarize(ctx, state.Thread.Messages)
	if err != nil {
		return err
	}
	state.Thread.Messages = updated
	return nil
}

func (a *Agent) callModel(ctx context.Context, state *State) error {
	contextMessages, err := a.buildContextMessages(state)
	if err != nil {
		return err
	}
	inputs, err := llm.MessagesToInput(contextMessages)
	if err != nil {
		return err
	}
	instructions := strings.TrimSpace(a.systemPrompt)
	toolChoice := responses.ResponseNewParamsToolChoiceUnion{}
	if state.ForceToolCall {
		toolChoice.OfToolChoiceMode = openai.Opt(responses.ToolChoiceOptionsRequired)
	}

	itemID := uuid.NewString()
	var onDelta func(string)
	if state.EventSink != nil {
		threadID := state.Thread.ID
		turnID := state.TurnID
		onDelta = func(delta string) {
			state.EventSink(Event{Type: EventModelDelta, ThreadID: threadID, TurnID: turnID, ItemID: itemID, Delta: delta})
		}
	}

	response, err := a.llm.CreateResponse(ctx, instructions, inputs, state.Tools, toolChoice, state.Stream, onDelta)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(response.OutputText())
	toolCalls := llm.ExtractToolCalls(response)

	if text == "" && len(toolCalls) == 0 {
		return errors.New("model returned no content")
	}

	if text != "" {
		msg := message.NewAIMessage(text)
		record, err := a.recordFromMessage(itemID, msg)
		if err != nil {
			return err
		}
		state.Thread.Messages = append(state.Thread.Messages, record)
		state.LastAssistant = text
	}
	if len(toolCalls) > 0 {
		msg := message.NewToolCallMessage(toolCalls)
		record, err := a.recordFromMessage("", msg)
		if err != nil {
			return err
		}
		state.Thread.Messages = append(state.Thread.Messages, record)
		state.PendingToolCalls = toolCalls
	}
	state.ForceToolCall = false
	return nil
}

func (a *Agent) route(ctx context.Context, state *State) (StageID, error) {
	select {
	case <-ctx.Done():
		return StageDone, ctx.Err()
	default:
	}
	if len(state.PendingToolCalls) > 0 {
		return StageCallTools, nil
	}
	if state.RestrictOutput {
		state.RestrictAttempts++
		if state.RestrictAttempts > a.maxRestrictAttempts {
			return StageDone, errors.New("restrict output attempts exceeded")
		}
		state.ForceToolCall = true
		return StageCallModel, nil
	}
	return StageSave, nil
}

func (a *Agent) callTools(ctx context.Context, state *State) error {
	if a.mcp == nil {
		return errors.New("mcp client is not configured")
	}
	for _, call := range state.PendingToolCalls {
		if state.EventSink != nil {
			state.EventSink(Event{Type: EventItemStarted, ThreadID: state.Thread.ID, TurnID: state.TurnID, ItemID: call.ID, ToolName: call.Name})
		}
		result, err := a.mcp.CallTool(ctx, mcp.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
		if err != nil {
			return err
		}
		output := message.ToolCallOutput{
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Output:     result.Content,
		}
		msg := message.NewToolCallOutputMessage(output)
		record, err := a.recordFromMessage("", msg)
		if err != nil {
			return err
		}
		state.Thread.Messages = append(state.Thread.Messages, record)
		if state.EventSink != nil {
			state.EventSink(Event{Type: EventItemDone, ThreadID: state.Thread.ID, TurnID: state.TurnID, ItemID: call.ID, ToolName: call.Name})
		}
	}
	state.PendingToolCalls = nil
	return nil
}

func (a *Agent) save(ctx context.Context, state *State) error {
	if strings.TrimSpace(state.LastAssistant) == "" {
		return errors.New("no assistant response to save")
	}
	state.Thread.UpdatedAt = time.Now().UTC()
	return a.store.Save(ctx, state.Thread)
}

func (a *Agent) recordFromMessage(recordID string, msg message.Message) (state.MessageRecord, error) {
	count, err := a.summarizer.CountTokens(msg)
	if err != nil {
		return state.MessageRecord{}, err
	}
	if strings.TrimSpace(recordID) == "" {
		recordID = uuid.NewString()
	}
	return state.MessageRecord{
		ID:         recordID,
		CreatedAt:  time.Now().UTC(),
		TokenCount: count,
		Message:    msg,
	}, nil
}

func (a *Agent) buildContextMessages(state *State) ([]message.Message, error) {
	contextMessages := make([]message.Message, 0, len(state.Thread.Messages))
	for _, record := range state.Thread.Messages {
		if !message.IsContextMessage(record.Message) {
			continue
		}
		contextMessages = append(contextMessages, record.Message)
	}
	if len(contextMessages) == 0 {
		return nil, fmt.Errorf("no context messages available")
	}
	return contextMessages, nil
}
