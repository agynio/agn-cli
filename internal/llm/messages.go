package llm

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
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
		textOutput, outputItems, textOnly, err := functionCallOutputPayload(typed.Output.Output)
		if err != nil {
			return nil, err
		}
		if textOnly {
			return []responses.ResponseInputItemUnionParam{
				responses.ResponseInputItemParamOfFunctionCallOutput(typed.Output.ToolCallID, textOutput),
			}, nil
		}
		return []responses.ResponseInputItemUnionParam{
			responses.ResponseInputItemParamOfFunctionCallOutput(typed.Output.ToolCallID, outputItems),
		}, nil
	case message.ResponseMessage:
		return nil, errors.New("response messages are not valid LLM input")
	default:
		return nil, fmt.Errorf("unsupported message type %T", msg)
	}
}

func functionCallOutputPayload(content []mcp.ContentItem) (string, responses.ResponseFunctionCallOutputItemListParam, bool, error) {
	if len(content) == 0 {
		return "", nil, false, errors.New("tool output content is empty")
	}
	textOnly := true
	textParts := make([]string, 0, len(content))
	textPresent := false
	for _, item := range content {
		if item.Type != mcp.ContentTypeText {
			textOnly = false
			continue
		}
		textParts = append(textParts, item.Text)
		if strings.TrimSpace(item.Text) != "" {
			textPresent = true
		}
	}
	if textOnly {
		if !textPresent {
			return "", nil, false, errors.New("tool output content is empty")
		}
		return strings.Join(textParts, "\n"), nil, true, nil
	}
	items := make(responses.ResponseFunctionCallOutputItemListParam, 0, len(content))
	for _, item := range content {
		switch item.Type {
		case mcp.ContentTypeText:
			items = append(items, responses.ResponseFunctionCallOutputItemParamOfInputText(item.Text))
		case mcp.ContentTypeImage:
			imageURL, err := imageDataURL(item)
			if err != nil {
				return "", nil, false, err
			}
			items = append(items, responses.ResponseFunctionCallOutputItemUnionParam{
				OfInputImage: &responses.ResponseInputImageContentParam{
					ImageURL: openai.Opt(imageURL),
				},
			})
		case mcp.ContentTypeAudio:
			fileItem, err := audioFileItem(item)
			if err != nil {
				return "", nil, false, err
			}
			items = append(items, fileItem)
		case mcp.ContentTypeResource:
			fileItem, err := resourceFileItem(item)
			if err != nil {
				return "", nil, false, err
			}
			items = append(items, fileItem)
		default:
			return "", nil, false, fmt.Errorf("unsupported content type %q", item.Type)
		}
	}
	return "", items, false, nil
}

func imageDataURL(item mcp.ContentItem) (string, error) {
	mimeType := strings.TrimSpace(item.MIMEType)
	if mimeType == "" {
		return "", errors.New("image mime type is required")
	}
	data := strings.TrimSpace(item.Data)
	if data == "" {
		return "", errors.New("image data is required")
	}
	return fmt.Sprintf("data:%s;base64,%s", mimeType, data), nil
}

func audioFileItem(item mcp.ContentItem) (responses.ResponseFunctionCallOutputItemUnionParam, error) {
	mimeType := strings.TrimSpace(item.MIMEType)
	if mimeType == "" {
		return responses.ResponseFunctionCallOutputItemUnionParam{}, errors.New("audio mime type is required")
	}
	data := strings.TrimSpace(item.Data)
	if data == "" {
		return responses.ResponseFunctionCallOutputItemUnionParam{}, errors.New("audio data is required")
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

func resourceFileItem(item mcp.ContentItem) (responses.ResponseFunctionCallOutputItemUnionParam, error) {
	resource := item.Resource
	if resource == nil {
		return responses.ResponseFunctionCallOutputItemUnionParam{}, errors.New("resource content is required")
	}
	filename, err := filenameFromURI(resource.URI)
	if err != nil {
		return responses.ResponseFunctionCallOutputItemUnionParam{}, err
	}
	data := strings.TrimSpace(resource.Blob)
	if data == "" {
		text := strings.TrimSpace(resource.Text)
		if text == "" {
			return responses.ResponseFunctionCallOutputItemUnionParam{}, errors.New("resource content is empty")
		}
		data = base64.StdEncoding.EncodeToString([]byte(text))
	}
	return responses.ResponseFunctionCallOutputItemUnionParam{
		OfInputFile: &responses.ResponseInputFileContentParam{
			FileData: openai.Opt(data),
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
		return "", errors.New("mime type is required")
	}
	extensions, err := mime.ExtensionsByType(trimmed)
	if err == nil && len(extensions) > 0 {
		return base + extensions[0], nil
	}
	if ext, ok := fallbackExtensions[trimmed]; ok {
		return base + ext, nil
	}
	return "", fmt.Errorf("unsupported mime type %q", mimeType)
}

var fallbackExtensions = map[string]string{
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
			Strict:     openai.Bool(true),
		}
		if strings.TrimSpace(tool.Description) != "" {
			function.Description = openai.String(tool.Description)
		}
		result = append(result, responses.ToolUnionParam{OfFunction: &function})
	}
	return result, nil
}
