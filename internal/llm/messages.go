package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/agynio/agn-cli/internal/mcp"
	"github.com/agynio/agn-cli/internal/message"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

func MessagesToInput(messages []message.Message) ([]responses.ResponseInputItemUnionParam, error) {
	inputs := make([]responses.ResponseInputItemUnionParam, 0, len(messages))
	for _, msg := range messages {
		items, err := messageToInputItems(msg)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, items...)
	}
	return inputs, nil
}

func messageToInputItems(msg message.Message) ([]responses.ResponseInputItemUnionParam, error) {
	switch typed := msg.(type) {
	case message.SystemMessage:
		return []responses.ResponseInputItemUnionParam{
			responses.ResponseInputItemParamOfMessage(typed.Text, responses.EasyInputMessageRoleSystem),
		}, nil
	case message.HumanMessage:
		return []responses.ResponseInputItemUnionParam{
			responses.ResponseInputItemParamOfMessage(typed.Text, responses.EasyInputMessageRoleUser),
		}, nil
	case message.AIMessage:
		return []responses.ResponseInputItemUnionParam{
			responses.ResponseInputItemParamOfMessage(typed.Text, responses.EasyInputMessageRoleAssistant),
		}, nil
	case message.ToolCallMessage:
		items := make([]responses.ResponseInputItemUnionParam, 0, len(typed.ToolCalls))
		for _, call := range typed.ToolCalls {
			items = append(items, responses.ResponseInputItemParamOfFunctionCall(call.Arguments, call.ID, call.Name))
		}
		return items, nil
	case message.ToolCallOutputMessage:
		outputPayload, err := functionCallOutputPayload(typed.Output.Output)
		if err != nil {
			return nil, err
		}
		functionCallOutput := responses.ResponseInputItemFunctionCallOutputParam{
			CallID: typed.Output.ToolCallID,
			Output: outputPayload,
		}
		return []responses.ResponseInputItemUnionParam{
			{OfFunctionCallOutput: &functionCallOutput},
		}, nil
	case message.ResponseMessage:
		return nil, errors.New("response messages are not valid LLM input")
	default:
		return nil, fmt.Errorf("unsupported message type %T", msg)
	}
}

func functionCallOutputPayload(content []mcp.ContentItem) (responses.ResponseInputItemFunctionCallOutputOutputUnionParam, error) {
	if len(content) == 0 {
		panic("tool output content is empty")
	}

	output := responses.ResponseInputItemFunctionCallOutputOutputUnionParam{}
	textOnly := true
	textParts := make([]string, 0, len(content))
	for _, item := range content {
		if item.Type != mcp.ContentTypeText {
			textOnly = false
			break
		}
		textParts = append(textParts, item.Text)
	}
	if textOnly {
		output.OfString = openai.Opt(strings.Join(textParts, "\n"))
		return output, nil
	}

	items := make(responses.ResponseFunctionCallOutputItemListParam, 0, len(content))
	for _, item := range content {
		switch item.Type {
		case mcp.ContentTypeText:
			items = append(items, responses.ResponseFunctionCallOutputItemParamOfInputText(item.Text))
		case mcp.ContentTypeImage:
			items = append(items, responses.ResponseFunctionCallOutputItemUnionParam{
				OfInputImage: &responses.ResponseInputImageContentParam{
					ImageURL: openai.Opt(imageDataURL(item)),
				},
			})
		case mcp.ContentTypeAudio:
			fileItem, err := audioFileItem(item)
			if err != nil {
				return output, err
			}
			items = append(items, fileItem)
		case mcp.ContentTypeResource:
			resourceItem, err := resourceOutputItem(item)
			if err != nil {
				return output, err
			}
			items = append(items, resourceItem)
		default:
			return output, fmt.Errorf("unsupported content type %q", item.Type)
		}
	}

	output.OfResponseFunctionCallOutputItemArray = items
	return output, nil
}

func imageDataURL(item mcp.ContentItem) string {
	mimeType := strings.TrimSpace(item.MIMEType)
	data := strings.TrimSpace(item.Data)
	if mimeType == "" || data == "" {
		panic("image content is missing required fields")
	}
	return fmt.Sprintf("data:%s;base64,%s", mimeType, data)
}

func audioFileItem(item mcp.ContentItem) (responses.ResponseFunctionCallOutputItemUnionParam, error) {
	mimeType := strings.TrimSpace(item.MIMEType)
	data := strings.TrimSpace(item.Data)
	if mimeType == "" || data == "" {
		panic("audio content is missing required fields")
	}
	filename, err := filenameFromMIME(mimeType, "audio")
	if err != nil {
		return responses.ResponseFunctionCallOutputItemUnionParam{}, err
	}
	return responses.ResponseFunctionCallOutputItemUnionParam{
		OfInputFile: &responses.ResponseInputFileContentParam{
			FileData: openai.Opt(data),
			Filename: openai.Opt(filename),
		},
	}, nil
}

func resourceOutputItem(item mcp.ContentItem) (responses.ResponseFunctionCallOutputItemUnionParam, error) {
	resource := item.Resource
	if resource == nil {
		panic("resource content is required")
	}
	text := strings.TrimSpace(resource.Text)
	blob := strings.TrimSpace(resource.Blob)
	if text != "" {
		if blob != "" {
			panic("resource content has both text and blob")
		}
		return responses.ResponseFunctionCallOutputItemParamOfInputText(text), nil
	}
	if blob == "" {
		panic("resource content is missing required fields")
	}
	filename, err := filenameFromURI(resource.URI)
	if err != nil {
		return responses.ResponseFunctionCallOutputItemUnionParam{}, err
	}
	return responses.ResponseFunctionCallOutputItemUnionParam{
		OfInputFile: &responses.ResponseInputFileContentParam{
			FileData: openai.Opt(blob),
			Filename: openai.Opt(filename),
		},
	}, nil
}

func filenameFromURI(rawURI string) (string, error) {
	trimmed := strings.TrimSpace(rawURI)
	if trimmed == "" {
		return "", errors.New("resource uri is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse resource uri: %w", err)
	}
	base := path.Base(parsed.Path)
	if base == "." || base == "/" || base == "" {
		base = path.Base(parsed.Opaque)
	}
	if base == "." || base == "/" || base == "" {
		return "", errors.New("resource filename is required")
	}
	return base, nil
}

func filenameFromMIME(mimeType, base string) (string, error) {
	trimmed := strings.TrimSpace(mimeType)
	if trimmed == "" {
		panic("mime type is required")
	}
	if ext, ok := mimeExtensions[trimmed]; ok {
		return base + ext, nil
	}
	return "", fmt.Errorf("unsupported mime type %q", mimeType)
}

var mimeExtensions = map[string]string{
	"audio/aac":   ".aac",
	"audio/flac":  ".flac",
	"audio/mp3":   ".mp3",
	"audio/mpeg":  ".mp3",
	"audio/mp4":   ".m4a",
	"audio/ogg":   ".ogg",
	"audio/wav":   ".wav",
	"audio/wave":  ".wav",
	"audio/x-wav": ".wav",
}

func ToolDefinitionsFromMCP(tools []mcp.Tool) ([]responses.ToolUnionParam, error) {
	result := make([]responses.ToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == "" {
			return nil, errors.New("tool name is required")
		}
		parameters := map[string]any{}
		if len(tool.InputSchema) > 0 {
			if err := json.Unmarshal(tool.InputSchema, &parameters); err != nil {
				return nil, fmt.Errorf("parse tool schema for %s: %w", tool.Name, err)
			}
		}
		function := responses.FunctionToolParam{
			Name:       tool.Name,
			Parameters: parameters,
			Strict:     openai.Bool(false),
		}
		if strings.TrimSpace(tool.Description) != "" {
			function.Description = openai.String(tool.Description)
		}
		result = append(result, responses.ToolUnionParam{OfFunction: &function})
	}
	return result, nil
}
