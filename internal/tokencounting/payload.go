package tokencounting

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/agynio/agn-cli/internal/mcp"
	"github.com/agynio/agn-cli/internal/message"
)

const (
	itemTypeMessage            = "message"
	itemTypeFunctionCall       = "function_call"
	itemTypeFunctionCallOutput = "function_call_output"

	contentTypeInputText  = "input_text"
	contentTypeOutputText = "output_text"
	contentTypeRefusal    = "refusal"
	contentTypeInputImage = "input_image"
	contentTypeInputFile  = "input_file"
	contentTypeInputAudio = "input_audio"
)

type tokenCountingMessageItem struct {
	Type    string                     `json:"type"`
	Role    string                     `json:"role"`
	Content []tokenCountingContentPart `json:"content"`
}

type tokenCountingFunctionCallItem struct {
	Type      string `json:"type"`
	Arguments string `json:"arguments"`
}

type tokenCountingFunctionCallOutputItem struct {
	Type   string `json:"type"`
	Output any    `json:"output"`
}

type tokenCountingContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Refusal  string `json:"refusal,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	FileID   string `json:"file_id,omitempty"`
	Detail   string `json:"detail,omitempty"`
	FileData string `json:"file_data,omitempty"`
	Filename string `json:"filename,omitempty"`
}

func tokenCountingItems(msg message.Message) ([]any, error) {
	switch typed := msg.(type) {
	case message.SystemMessage:
		return []any{messageItem(typed.Role(), typed.Text)}, nil
	case message.HumanMessage:
		return []any{messageItem(typed.Role(), typed.Text)}, nil
	case message.AIMessage:
		return []any{messageItem(typed.Role(), typed.Text)}, nil
	case message.ToolCallMessage:
		if len(typed.ToolCalls) == 0 {
			return nil, errors.New("tool calls are required for token counting")
		}
		items := make([]any, 0, len(typed.ToolCalls))
		for _, call := range typed.ToolCalls {
			items = append(items, tokenCountingFunctionCallItem{
				Type:      itemTypeFunctionCall,
				Arguments: call.Arguments,
			})
		}
		return items, nil
	case message.ToolCallOutputMessage:
		output, err := functionCallOutputPayload(typed.Output)
		if err != nil {
			return nil, err
		}
		return []any{tokenCountingFunctionCallOutputItem{Type: itemTypeFunctionCallOutput, Output: output}}, nil
	case message.ResponseMessage:
		return nil, errors.New("response messages are not valid for token counting")
	default:
		return nil, fmt.Errorf("unsupported message type %T", msg)
	}
}

func messageItem(role message.Role, text string) tokenCountingMessageItem {
	return tokenCountingMessageItem{
		Type: itemTypeMessage,
		Role: string(role),
		Content: []tokenCountingContentPart{
			{Type: contentTypeInputText, Text: text},
		},
	}
}

func functionCallOutputPayload(output message.ToolCallOutput) (any, error) {
	if len(output.Output) == 0 {
		return nil, errors.New("tool output content is required for token counting")
	}
	textOnly := true
	textParts := make([]string, 0, len(output.Output))
	for _, item := range output.Output {
		if item.Type != mcp.ContentTypeText {
			textOnly = false
			break
		}
		textParts = append(textParts, item.Text)
	}
	if textOnly {
		return strings.Join(textParts, "\n"), nil
	}
	parts := make([]tokenCountingContentPart, 0, len(output.Output))
	for _, item := range output.Output {
		part, err := contentPartFromMCP(item)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return parts, nil
}

func contentPartFromMCP(item mcp.ContentItem) (tokenCountingContentPart, error) {
	switch item.Type {
	case mcp.ContentTypeText:
		return tokenCountingContentPart{Type: contentTypeInputText, Text: item.Text}, nil
	case mcp.ContentTypeImage:
		imageURL, err := imageDataURL(item)
		if err != nil {
			return tokenCountingContentPart{}, err
		}
		return tokenCountingContentPart{Type: contentTypeInputImage, ImageURL: imageURL}, nil
	case mcp.ContentTypeAudio:
		filename, err := filenameFromMIME(item.MIMEType, "audio")
		if err != nil {
			return tokenCountingContentPart{}, err
		}
		data := strings.TrimSpace(item.Data)
		if data == "" {
			return tokenCountingContentPart{}, errors.New("audio data is required")
		}
		return tokenCountingContentPart{Type: contentTypeInputFile, FileData: data, Filename: filename}, nil
	case mcp.ContentTypeResource:
		resource := item.Resource
		if resource == nil {
			return tokenCountingContentPart{}, errors.New("resource content is required")
		}
		text := strings.TrimSpace(resource.Text)
		blob := strings.TrimSpace(resource.Blob)
		if text != "" {
			if blob != "" {
				return tokenCountingContentPart{}, errors.New("resource content has both text and blob")
			}
			return tokenCountingContentPart{Type: contentTypeInputText, Text: text}, nil
		}
		if blob == "" {
			return tokenCountingContentPart{}, errors.New("resource content is empty")
		}
		filename, err := filenameFromURI(resource.URI)
		if err != nil {
			return tokenCountingContentPart{}, err
		}
		return tokenCountingContentPart{Type: contentTypeInputFile, FileData: blob, Filename: filename}, nil
	default:
		if strings.TrimSpace(string(item.Type)) == "" {
			return tokenCountingContentPart{}, errors.New("content type is required")
		}
		return tokenCountingContentPart{}, fmt.Errorf("unsupported content type %q", item.Type)
	}
}

func imageDataURL(item mcp.ContentItem) (string, error) {
	mimeType := strings.TrimSpace(item.MIMEType)
	data := strings.TrimSpace(item.Data)
	if mimeType == "" || data == "" {
		return "", errors.New("image content is missing required fields")
	}
	return fmt.Sprintf("data:%s;base64,%s", mimeType, data), nil
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
