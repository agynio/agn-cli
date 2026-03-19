package llm

import (
	"strings"

	"github.com/agynio/agn-cli/internal/message"
)

func (r Response) Text() string {
	var builder strings.Builder
	for _, item := range r.Output {
		if item.Type != "message" && item.Role != "assistant" {
			continue
		}
		for _, content := range item.Content {
			switch content.Type {
			case "output_text", "input_text", "text":
				builder.WriteString(content.Text)
			}
		}
	}
	return builder.String()
}

func (r Response) ToolCalls() []message.ToolCall {
	var calls []message.ToolCall
	for _, item := range r.Output {
		if item.Type == "tool_call" {
			calls = append(calls, message.ToolCall{
				ID:        item.ID,
				Name:      item.Name,
				Arguments: item.Arguments,
			})
			continue
		}
		for _, call := range item.ToolCalls {
			calls = append(calls, message.ToolCall{
				ID:        call.ID,
				Name:      call.Name,
				Arguments: call.Arguments,
			})
		}
	}
	return calls
}
