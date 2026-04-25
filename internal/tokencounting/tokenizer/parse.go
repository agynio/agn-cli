package tokenizer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func ParseMessage(payload []byte) (Message, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return Message{}, errors.New("message is empty")
	}

	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return Message{}, fmt.Errorf("parse message json: %w", err)
	}

	switch ItemType(envelope.Type) {
	case ItemTypeMessage:
		item, err := parseMessageItem(trimmed)
		if err != nil {
			return Message{}, err
		}
		return NewMessage(item), nil
	case ItemTypeFunctionCall:
		item, err := parseFunctionCallItem(trimmed)
		if err != nil {
			return Message{}, err
		}
		return NewMessage(item), nil
	case ItemTypeFunctionCallOutput:
		item, err := parseFunctionCallOutputItem(trimmed)
		if err != nil {
			return Message{}, err
		}
		return NewMessage(item), nil
	case "":
		return Message{}, errors.New("message type is required")
	default:
		return Message{}, fmt.Errorf("unsupported message type %q", envelope.Type)
	}
}

func parseMessageItem(payload []byte) (MessageItem, error) {
	var raw struct {
		Role    *string           `json:"role"`
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return MessageItem{}, fmt.Errorf("parse message item: %w", err)
	}
	if raw.Role == nil {
		return MessageItem{}, errors.New("message role is required")
	}
	role := MessageRole(strings.TrimSpace(*raw.Role))
	if role == "" {
		return MessageItem{}, errors.New("message role is required")
	}
	if !isValidRole(role) {
		return MessageItem{}, fmt.Errorf("unsupported message role %q", role)
	}
	if raw.Content == nil {
		return MessageItem{}, errors.New("message content is required")
	}

	content := make([]ContentPart, 0, len(raw.Content))
	for i, part := range raw.Content {
		parsed, err := parseContentPart(part)
		if err != nil {
			return MessageItem{}, fmt.Errorf("content[%d]: %w", i, err)
		}
		content = append(content, parsed)
	}

	return MessageItem{Role: role, Content: content}, nil
}

func parseFunctionCallItem(payload []byte) (FunctionCallItem, error) {
	var raw struct {
		Arguments *string `json:"arguments"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return FunctionCallItem{}, fmt.Errorf("parse function call item: %w", err)
	}
	if raw.Arguments == nil {
		return FunctionCallItem{}, errors.New("function call arguments are required")
	}
	return FunctionCallItem{Arguments: *raw.Arguments}, nil
}

func parseFunctionCallOutputItem(payload []byte) (FunctionCallOutputItem, error) {
	var raw struct {
		Output json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return FunctionCallOutputItem{}, fmt.Errorf("parse function call output item: %w", err)
	}
	if len(raw.Output) == 0 {
		return FunctionCallOutputItem{}, errors.New("function call output is required")
	}

	output, err := parseFunctionCallOutput(raw.Output)
	if err != nil {
		return FunctionCallOutputItem{}, err
	}
	return FunctionCallOutputItem{Output: output}, nil
}

func parseFunctionCallOutput(raw json.RawMessage) (FunctionCallOutput, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return FunctionCallOutput{}, errors.New("function call output is required")
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return FunctionCallOutput{}, fmt.Errorf("parse function call output string: %w", err)
		}
		return FunctionCallOutput{Text: text, IsText: true}, nil
	}
	if trimmed[0] != '[' {
		return FunctionCallOutput{}, errors.New("function call output must be string or array")
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(trimmed, &parts); err != nil {
		return FunctionCallOutput{}, fmt.Errorf("parse function call output array: %w", err)
	}
	content := make([]ContentPart, 0, len(parts))
	for i, part := range parts {
		parsed, err := parseContentPart(part)
		if err != nil {
			return FunctionCallOutput{}, fmt.Errorf("output[%d]: %w", i, err)
		}
		content = append(content, parsed)
	}
	return FunctionCallOutput{Content: content}, nil
}

func parseContentPart(raw json.RawMessage) (ContentPart, error) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ContentPart{}, fmt.Errorf("parse content type: %w", err)
	}
	if strings.TrimSpace(envelope.Type) == "" {
		return ContentPart{}, errors.New("content type is required")
	}

	switch ContentType(envelope.Type) {
	case ContentTypeInputText, ContentTypeOutputText:
		var content struct {
			Text *string `json:"text"`
		}
		if err := json.Unmarshal(raw, &content); err != nil {
			return ContentPart{}, fmt.Errorf("parse text content: %w", err)
		}
		if content.Text == nil {
			return ContentPart{}, errors.New("text content is required")
		}
		return ContentPart{Type: ContentType(envelope.Type), Text: *content.Text}, nil
	case ContentTypeRefusal:
		var content struct {
			Refusal *string `json:"refusal"`
		}
		if err := json.Unmarshal(raw, &content); err != nil {
			return ContentPart{}, fmt.Errorf("parse refusal content: %w", err)
		}
		if content.Refusal == nil {
			return ContentPart{}, errors.New("refusal content is required")
		}
		return ContentPart{Type: ContentTypeRefusal, Text: *content.Refusal}, nil
	case ContentTypeInputImage:
		var content struct {
			ImageURL *string `json:"image_url"`
			FileID   *string `json:"file_id"`
			Detail   string  `json:"detail"`
		}
		if err := json.Unmarshal(raw, &content); err != nil {
			return ContentPart{}, fmt.Errorf("parse image content: %w", err)
		}
		imageURL := strings.TrimSpace(optString(content.ImageURL))
		fileID := strings.TrimSpace(optString(content.FileID))
		if imageURL == "" && fileID == "" {
			return ContentPart{}, errors.New("image content requires image_url or file_id")
		}
		detailValue := strings.ToLower(strings.TrimSpace(content.Detail))
		detail := ImageDetailHigh
		switch detailValue {
		case "":
			// default high
		case string(ImageDetailHigh):
			detail = ImageDetailHigh
		case string(ImageDetailLow):
			detail = ImageDetailLow
		default:
			return ContentPart{}, fmt.Errorf("unsupported image detail %q", content.Detail)
		}
		return ContentPart{Type: ContentTypeInputImage, Image: ImageContent{ImageURL: imageURL, FileID: fileID, Detail: detail}}, nil
	case ContentTypeInputFile:
		var content struct {
			FileData *string `json:"file_data"`
			Filename *string `json:"filename"`
		}
		if err := json.Unmarshal(raw, &content); err != nil {
			return ContentPart{}, fmt.Errorf("parse file content: %w", err)
		}
		if content.FileData == nil || strings.TrimSpace(*content.FileData) == "" {
			return ContentPart{}, errors.New("file_data is required")
		}
		if content.Filename == nil || strings.TrimSpace(*content.Filename) == "" {
			return ContentPart{}, errors.New("filename is required")
		}
		return ContentPart{Type: ContentTypeInputFile, File: FileContent{Filename: strings.TrimSpace(*content.Filename), Data: strings.TrimSpace(*content.FileData)}}, nil
	case ContentTypeInputAudio:
		return ContentPart{Type: ContentTypeInputAudio}, nil
	default:
		return ContentPart{}, fmt.Errorf("unsupported content type %q", envelope.Type)
	}
}

func optString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func isValidRole(role MessageRole) bool {
	switch role {
	case RoleSystem, RoleUser, RoleAssistant, RoleTool:
		return true
	default:
		return false
	}
}
