package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type ContentType string

const (
	ContentTypeText     ContentType = "text"
	ContentTypeImage    ContentType = "image"
	ContentTypeAudio    ContentType = "audio"
	ContentTypeResource ContentType = "resource"
)

type ContentItem struct {
	Type     ContentType      `json:"type"`
	Text     string           `json:"text,omitempty"`
	MIMEType string           `json:"mimeType,omitempty"`
	Data     string           `json:"data,omitempty"`
	Resource *ResourceContent `json:"resource,omitempty"`
}

type ResourceContent struct {
	URI      string `json:"uri"`
	MIMEType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

func ParseContentItems(raw json.RawMessage) ([]ContentItem, error) {
	if len(raw) == 0 {
		return nil, errors.New("tool result content is empty")
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, errors.New("tool result content is empty")
	}

	var items []ContentItem
	if err := json.Unmarshal(raw, &items); err == nil {
		if err := ValidateContentItems(items); err != nil {
			return nil, err
		}
		return items, nil
	}

	var single ContentItem
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, fmt.Errorf("parse tool content: %w", err)
	}
	items = []ContentItem{single}
	if err := ValidateContentItems(items); err != nil {
		return nil, err
	}
	return items, nil
}

func ValidateContentItems(items []ContentItem) error {
	if len(items) == 0 {
		return errors.New("tool result content is empty")
	}
	hasText := false
	hasTextValue := false
	hasNonText := false
	for _, item := range items {
		switch item.Type {
		case ContentTypeText:
			hasText = true
			if strings.TrimSpace(item.Text) != "" {
				hasTextValue = true
			}
		case ContentTypeImage:
			hasNonText = true
			if strings.TrimSpace(item.MIMEType) == "" {
				return errors.New("image mime type is required")
			}
			if strings.TrimSpace(item.Data) == "" {
				return errors.New("image data is required")
			}
		case ContentTypeAudio:
			hasNonText = true
			if strings.TrimSpace(item.MIMEType) == "" {
				return errors.New("audio mime type is required")
			}
			if strings.TrimSpace(item.Data) == "" {
				return errors.New("audio data is required")
			}
		case ContentTypeResource:
			hasNonText = true
			if item.Resource == nil {
				return errors.New("resource content is required")
			}
			if strings.TrimSpace(item.Resource.URI) == "" {
				return errors.New("resource uri is required")
			}
			hasTextResource := strings.TrimSpace(item.Resource.Text) != ""
			hasBlobResource := strings.TrimSpace(item.Resource.Blob) != ""
			if hasTextResource && hasBlobResource {
				return errors.New("resource cannot contain both text and blob")
			}
			if !hasTextResource && !hasBlobResource {
				return errors.New("resource content is empty")
			}
		default:
			if strings.TrimSpace(string(item.Type)) == "" {
				return errors.New("content type is required")
			}
			return fmt.Errorf("unsupported content type %q", item.Type)
		}
	}
	if !hasNonText && hasText && !hasTextValue {
		return errors.New("tool result content is empty")
	}
	return nil
}
