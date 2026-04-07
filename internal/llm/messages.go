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

func sanitizeMCPToolSchema(schema map[string]any) {
	if schema == nil {
		return
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		for key, value := range properties {
			switch typed := value.(type) {
			case map[string]any:
				sanitizeMCPToolSchema(typed)
			case bool:
				properties[key] = map[string]any{"type": "string"}
			}
		}
	}
	if items, ok := schema["items"].(map[string]any); ok {
		sanitizeMCPToolSchema(items)
	} else if _, ok := schema["items"].(bool); ok {
		schema["items"] = map[string]any{"type": "string"}
	}

	for _, combiner := range []string{"anyOf", "oneOf", "allOf", "prefixItems"} {
		if entries, ok := schema[combiner].([]any); ok {
			for i, entry := range entries {
				switch typed := entry.(type) {
				case map[string]any:
					sanitizeMCPToolSchema(typed)
				case bool:
					entries[i] = map[string]any{"type": "string"}
				}
			}
		}
	}

	resolvedType := resolveMCPToolSchemaType(schema)
	schema["type"] = resolvedType

	switch resolvedType {
	case "object":
		properties, ok := schema["properties"]
		if !ok || properties == nil {
			schema["properties"] = map[string]any{}
		}
		if additional, ok := schema["additionalProperties"]; ok {
			if additionalSchema, ok := additional.(map[string]any); ok {
				sanitizeMCPToolSchema(additionalSchema)
			}
		}
	case "array":
		items, ok := schema["items"]
		if !ok || items == nil {
			schema["items"] = map[string]any{"type": "string"}
		}
	}
}

func resolveMCPToolSchemaType(schema map[string]any) string {
	if rawType, ok := schema["type"]; ok && rawType != nil {
		switch typed := rawType.(type) {
		case string:
			return typed
		case []any:
			for _, entry := range typed {
				if entryType, ok := entry.(string); ok && isRecognizedSchemaType(entryType) {
					return entryType
				}
			}
		}
	}
	if schemaHasValue(schema, "properties") || schemaHasValue(schema, "required") || schemaHasValue(schema, "additionalProperties") {
		return "object"
	}
	if schemaHasValue(schema, "items") || schemaHasValue(schema, "prefixItems") {
		return "array"
	}
	if schemaHasValue(schema, "enum") || schemaHasValue(schema, "const") || schemaHasValue(schema, "format") {
		return "string"
	}
	if schemaHasValue(schema, "minimum") || schemaHasValue(schema, "maximum") || schemaHasValue(schema, "exclusiveMinimum") || schemaHasValue(schema, "exclusiveMaximum") || schemaHasValue(schema, "multipleOf") {
		return "number"
	}
	return "string"
}

func schemaHasValue(schema map[string]any, key string) bool {
	value, ok := schema[key]
	return ok && value != nil
}

func isRecognizedSchemaType(value string) bool {
	switch value {
	case "object", "array", "string", "number", "integer", "boolean":
		return true
	default:
		return false
	}
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
		sanitizeMCPToolSchema(parameters)
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
