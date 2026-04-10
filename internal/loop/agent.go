package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/agynio/agn-cli/internal/llm"
	"github.com/agynio/agn-cli/internal/mcp"
	"github.com/agynio/agn-cli/internal/message"
	"github.com/agynio/agn-cli/internal/state"
	"github.com/agynio/agn-cli/internal/summarize"
)

const (
	DefaultMaxSteps            = 1000
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
	Thread             state.Thread
	TurnID             string
	Input              *message.HumanMessage
	RestrictOutput     bool
	Stream             bool
	RestrictAttempts   int
	ForceToolCall      bool
	Tools              []responses.ToolUnionParam
	PendingToolCalls   []message.ToolCall
	LastAssistant      string
	EventSink          EventSink
	LoadedMessageCount int
}

type AgentConfig struct {
	Store               state.Store
	LLM                 *llm.Client
	Summarizer          *summarize.Summarizer
	MCP                 mcp.ToolProvider
	SystemPrompt        string
	MaxSteps            int
	MaxRestrictAttempts int
	Tracer              trace.Tracer
}

type Agent struct {
	store               state.Store
	llm                 *llm.Client
	summarizer          *summarize.Summarizer
	mcp                 mcp.ToolProvider
	systemPrompt        string
	maxRestrictAttempts int
	loop                *Loop
	tracer              trace.Tracer
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
		return nil, errors.New("max steps must be >= 1")
	}
	maxRestrict := cfg.MaxRestrictAttempts
	if maxRestrict <= 0 {
		maxRestrict = defaultMaxRestrictAttempts
	}
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = trace.NewNoopTracerProvider().Tracer("agn")
	}

	agent := &Agent{
		store:               cfg.Store,
		llm:                 cfg.LLM,
		summarizer:          cfg.Summarizer,
		mcp:                 cfg.MCP,
		systemPrompt:        cfg.SystemPrompt,
		maxRestrictAttempts: maxRestrict,
		tracer:              tracer,
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
	spanCtx, span := a.tracer.Start(ctx, "invocation.message")
	span.SetAttributes(
		attribute.String("agyn.message.text", input.Prompt.Text),
		attribute.String("agyn.message.role", "user"),
		attribute.String("agyn.message.kind", "source"),
	)
	defer span.End()
	state := &State{
		Thread:         state.Thread{ID: threadID, Messages: []state.MessageRecord{}},
		TurnID:         turnID,
		Input:          &input.Prompt,
		RestrictOutput: input.RestrictOutput,
		Stream:         input.Stream,
		EventSink:      input.EventSink,
	}
	if err := a.loop.Run(spanCtx, state); err != nil {
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
	state.LoadedMessageCount = len(thread.Messages)
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
	start := time.Now()
	previousCount := len(state.Thread.Messages)
	previousLoaded := state.LoadedMessageCount
	result, err := a.summarizer.Summarize(ctx, state.Thread.Messages)
	if err != nil {
		return err
	}
	state.Thread.Messages = result.Messages
	if result.Performed {
		// Emit the span retroactively with captured timestamps to cover the
		// summarization duration without wrapping the call in a span.
		end := time.Now()
		_, span := a.tracer.Start(ctx, "summarization", trace.WithTimestamp(start))
		span.SetAttributes(
			attribute.String("agyn.summarization.text", result.SummaryText),
			attribute.Int("agyn.summarization.new_context_count", result.NewContextCount),
			attribute.Int("agyn.summarization.old_context_tokens", result.OldContextTokens),
		)
		span.End(trace.WithTimestamp(end))
		state.LoadedMessageCount = adjustLoadedMessageCount(
			previousCount,
			previousLoaded,
			result.NewContextCount,
			len(result.Messages),
		)
	}
	return nil
}

func (a *Agent) callModel(ctx context.Context, state *State) error {
	spanCtx, span := a.tracer.Start(ctx, "llm.call")
	defer span.End()
	span.SetAttributes(attribute.String("gen_ai.system", "openai"))

	contextItems, err := a.buildContextItems(state)
	if err != nil {
		return err
	}
	contextMessages := make([]message.Message, 0, len(contextItems))
	for _, item := range contextItems {
		text, err := contextItemText(item.Message)
		if err != nil {
			return err
		}
		span.AddEvent("agyn.llm.context_item", trace.WithAttributes(
			attribute.String("agyn.context.role", string(item.Message.Role())),
			attribute.String("agyn.context.text", text),
			attribute.String("agyn.context.is_new", strconv.FormatBool(item.IsNew)),
			attribute.Int("agyn.context.size_bytes", len(text)),
		))
		contextMessages = append(contextMessages, item.Message)
	}
	inputs, err := llm.MessagesToInput(contextMessages)
	if err != nil {
		return err
	}
	instructions := ""
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

	response, err := a.llm.CreateResponse(spanCtx, instructions, inputs, state.Tools, toolChoice, state.Stream, onDelta)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(response.OutputText())
	span.SetAttributes(
		attribute.String("gen_ai.request.model", response.Model),
		attribute.String("gen_ai.response.finish_reason", string(response.Status)),
		attribute.String("agyn.llm.response_text", text),
	)
	if response.JSON.Usage.Valid() {
		span.SetAttributes(
			attribute.Int64("gen_ai.usage.input_tokens", response.Usage.InputTokens),
			attribute.Int64("gen_ai.usage.output_tokens", response.Usage.OutputTokens),
			attribute.Int64("gen_ai.usage.cache_read.input_tokens", response.Usage.InputTokensDetails.CachedTokens),
			attribute.Int64("agyn.usage.reasoning_tokens", response.Usage.OutputTokensDetails.ReasoningTokens),
		)
	}
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
		if err := a.executeTool(ctx, call, state); err != nil {
			return err
		}
	}
	state.PendingToolCalls = nil
	return nil
}

func (a *Agent) executeTool(ctx context.Context, call message.ToolCall, state *State) error {
	spanCtx, span := a.tracer.Start(ctx, "tool.execution")
	defer span.End()
	span.SetAttributes(
		attribute.String("agyn.tool.name", call.Name),
		attribute.String("agyn.tool.call_id", call.ID),
		attribute.String("agyn.tool.input", call.Arguments),
	)
	if state.EventSink != nil {
		state.EventSink(Event{Type: EventItemStarted, ThreadID: state.Thread.ID, TurnID: state.TurnID, ItemID: call.ID, ToolName: call.Name})
	}
	result, err := a.mcp.CallTool(spanCtx, mcp.ToolCall{ID: call.ID, Name: call.Name, Arguments: json.RawMessage(call.Arguments)})
	if err != nil {
		return err
	}
	outputPayload, err := json.Marshal(result.Content)
	if err != nil {
		return err
	}
	span.SetAttributes(attribute.String("agyn.tool.output", string(outputPayload)))
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

func (a *Agent) buildContextItems(state *State) ([]contextItem, error) {
	contextItems := make([]contextItem, 0, len(state.Thread.Messages)+1)
	if strings.TrimSpace(a.systemPrompt) != "" {
		contextItems = append(contextItems, contextItem{Message: message.NewSystemMessage(a.systemPrompt), IsNew: false})
	}
	for index, record := range state.Thread.Messages {
		if !message.IsContextMessage(record.Message) {
			continue
		}
		contextItems = append(contextItems, contextItem{Message: record.Message, IsNew: index >= state.LoadedMessageCount})
	}
	if len(contextItems) == 0 {
		return nil, fmt.Errorf("no context messages available")
	}
	return contextItems, nil
}

type contextItem struct {
	Message message.Message
	IsNew   bool
}

func contextItemText(msg message.Message) (string, error) {
	switch typed := msg.(type) {
	case message.SystemMessage:
		return typed.Text, nil
	case message.HumanMessage:
		return typed.Text, nil
	case message.AIMessage:
		return typed.Text, nil
	case message.ToolCallMessage:
		payload, err := json.Marshal(typed.ToolCalls)
		if err != nil {
			return "", err
		}
		return string(payload), nil
	case message.ToolCallOutputMessage:
		payload, err := json.Marshal(typed.Output)
		if err != nil {
			return "", err
		}
		return string(payload), nil
	case message.ResponseMessage:
		return "", errors.New("response messages are not valid context items")
	default:
		return "", fmt.Errorf("unsupported message type %T", msg)
	}
}

func adjustLoadedMessageCount(previousCount, previousLoaded, newContextCount, newTotal int) int {
	if newTotal == newContextCount {
		return previousLoaded
	}
	newMessages := previousCount - previousLoaded
	oldKeptCount := newContextCount - newMessages
	if oldKeptCount < 0 {
		oldKeptCount = 0
	}
	adjusted := 1 + oldKeptCount
	if adjusted > newTotal {
		return newTotal
	}
	return adjusted
}
