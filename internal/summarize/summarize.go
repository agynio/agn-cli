package summarize

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/agynio/agn-cli/internal/llm"
	"github.com/agynio/agn-cli/internal/message"
	"github.com/agynio/agn-cli/internal/state"
	"github.com/openai/openai-go/v3/responses"
)

const (
	DefaultKeepTokens = 2048
	DefaultMaxTokens  = 4096
)

type TokenCounter func(message.Message) (int, error)

type Config struct {
	KeepTokens   int
	MaxTokens    int
	TokenCounter TokenCounter
}

type Summarizer struct {
	client       *llm.Client
	keepTokens   int
	maxTokens    int
	tokenCounter TokenCounter
}

func New(client *llm.Client, cfg Config) (*Summarizer, error) {
	if client == nil {
		return nil, errors.New("llm client is required")
	}
	keep := cfg.KeepTokens
	if keep <= 0 {
		keep = DefaultKeepTokens
	}
	max := cfg.MaxTokens
	if max <= 0 {
		max = DefaultMaxTokens
	}
	if keep > max {
		return nil, errors.New("keep tokens must be <= max tokens")
	}
	counter := cfg.TokenCounter
	if counter == nil {
		counter = DefaultTokenCounter
	}
	return &Summarizer{
		client:       client,
		keepTokens:   keep,
		maxTokens:    max,
		tokenCounter: counter,
	}, nil
}

func (s *Summarizer) CountTokens(msg message.Message) (int, error) {
	return s.tokenCounter(msg)
}

func (s *Summarizer) Summarize(ctx context.Context, messages []state.MessageRecord) ([]state.MessageRecord, error) {
	if len(messages) == 0 {
		return messages, nil
	}

	totalTokens := 0
	for _, record := range messages {
		if record.TokenCount <= 0 {
			return nil, errors.New("message token count missing")
		}
		totalTokens += record.TokenCount
	}
	if totalTokens <= s.maxTokens {
		return messages, nil
	}

	keepIndex := len(messages)
	keptTokens := 0
	for i := len(messages) - 1; i >= 0; i-- {
		keptTokens += messages[i].TokenCount
		keepIndex = i
		if keptTokens >= s.keepTokens {
			break
		}
	}
	if keepIndex <= 0 {
		return messages, nil
	}
	// Adjust keepIndex to avoid splitting tool_call/tool_output pairs.
	// If kept would start with tool_output messages, walk keepIndex backward
	// to include the preceding tool_call.
	for keepIndex > 0 && messages[keepIndex].Message.Kind() == message.KindToolCallOutput {
		keepIndex--
	}

	older := messages[:keepIndex]
	kept := messages[keepIndex:]
	input := renderSummaryInput(older)
	if strings.TrimSpace(input) == "" {
		return messages, nil
	}

	instructions := "Summarize the thread history. Keep decisions, tool usage, and requirements. Be concise."
	user := message.NewHumanMessage(input)
	inputs, err := llm.MessagesToInput([]message.Message{user})
	if err != nil {
		return nil, err
	}
	toolChoice := responses.ResponseNewParamsToolChoiceUnion{}
	response, err := s.client.CreateResponse(ctx, instructions, inputs, nil, toolChoice, false, nil)
	if err != nil {
		return nil, err
	}
	summaryText := strings.TrimSpace(response.OutputText())
	if summaryText == "" {
		return nil, errors.New("summary response was empty")
	}

	summary := message.NewSummaryMessage(summaryText)
	tokenCount, err := s.tokenCounter(summary)
	if err != nil {
		return nil, err
	}

	summaryRecord := state.MessageRecord{
		ID:         uuid.NewString(),
		CreatedAt:  older[len(older)-1].CreatedAt,
		TokenCount: tokenCount,
		Message:    summary,
	}

	result := make([]state.MessageRecord, 0, 1+len(kept))
	result = append(result, summaryRecord)
	result = append(result, kept...)
	return result, nil
}

func renderSummaryInput(records []state.MessageRecord) string {
	var builder strings.Builder
	for _, record := range records {
		text, ok := message.TextForSummary(record.Message)
		if !ok {
			continue
		}
		role := record.Message.Role()
		kind := record.Message.Kind()
		builder.WriteString(fmt.Sprintf("%s (%s): %s\n", role, kind, text))
	}
	return builder.String()
}

func DefaultTokenCounter(msg message.Message) (int, error) {
	text, ok := message.TextForSummary(msg)
	if !ok {
		return 1, nil
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 1, nil
	}
	runes := []rune(trimmed)
	return len(runes)/4 + 1, nil
}
