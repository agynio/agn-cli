package llm

import (
	"github.com/agynio/agn-cli/internal/message"
	"github.com/openai/openai-go/v3/responses"
)

func ExtractToolCalls(resp *responses.Response) []message.ToolCall {
	var calls []message.ToolCall
	for _, item := range resp.Output {
		if item.Type != "function_call" {
			continue
		}
		call := item.AsFunctionCall()
		calls = append(calls, message.ToolCall{
			ID:        call.CallID,
			Name:      call.Name,
			Arguments: call.Arguments,
		})
	}
	return calls
}
