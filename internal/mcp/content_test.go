package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseContentItems(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want []ContentItem
		err  string
	}{
		{
			name: "empty",
			raw:  nil,
			err:  "tool result content is empty",
		},
		{
			name: "null",
			raw:  json.RawMessage("null"),
			err:  "tool result content is empty",
		},
		{
			name: "empty array",
			raw:  json.RawMessage("[]"),
			err:  "tool result content is empty",
		},
		{
			name: "single object",
			raw:  json.RawMessage(`{"type":"text","text":"hello"}`),
			want: []ContentItem{{Type: ContentTypeText, Text: "hello"}},
		},
		{
			name: "array",
			raw:  json.RawMessage(`[{"type":"text","text":"hello"},{"type":"text","text":"world"}]`),
			want: []ContentItem{{Type: ContentTypeText, Text: "hello"}, {Type: ContentTypeText, Text: "world"}},
		},
		{
			name: "invalid array",
			raw:  json.RawMessage("[{]"),
			err:  "parse tool content",
		},
		{
			name: "invalid object",
			raw:  json.RawMessage("{"),
			err:  "parse tool content",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			items, err := ParseContentItems(tc.raw)
			if tc.err != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, items)
		})
	}
}

func TestValidateContentItems(t *testing.T) {
	tests := []struct {
		name  string
		items []ContentItem
		err   string
	}{
		{
			name:  "empty",
			items: nil,
			err:   "tool result content is empty",
		},
		{
			name:  "missing type",
			items: []ContentItem{{}},
			err:   "content type is required",
		},
		{
			name:  "unknown type",
			items: []ContentItem{{Type: "video"}},
			err:   "unsupported content type",
		},
		{
			name:  "text only empty",
			items: []ContentItem{{Type: ContentTypeText, Text: "  "}},
			err:   "tool result content is empty",
		},
		{
			name:  "image missing mime",
			items: []ContentItem{{Type: ContentTypeImage, Data: "ZmFrZQ=="}},
			err:   "image mime type is required",
		},
		{
			name:  "image missing data",
			items: []ContentItem{{Type: ContentTypeImage, MIMEType: "image/png"}},
			err:   "image data is required",
		},
		{
			name:  "audio missing mime",
			items: []ContentItem{{Type: ContentTypeAudio, Data: "c291bmQ="}},
			err:   "audio mime type is required",
		},
		{
			name:  "audio missing data",
			items: []ContentItem{{Type: ContentTypeAudio, MIMEType: "audio/wav"}},
			err:   "audio data is required",
		},
		{
			name:  "resource missing content",
			items: []ContentItem{{Type: ContentTypeResource}},
			err:   "resource content is required",
		},
		{
			name: "resource missing uri",
			items: []ContentItem{{
				Type: ContentTypeResource,
				Resource: &ResourceContent{
					Text: "report",
				},
			}},
			err: "resource uri is required",
		},
		{
			name: "resource has text and blob",
			items: []ContentItem{{
				Type: ContentTypeResource,
				Resource: &ResourceContent{
					URI:  "file:///report.txt",
					Text: "report",
					Blob: "cmVwb3J0",
				},
			}},
			err: "resource cannot contain both text and blob",
		},
		{
			name: "resource empty",
			items: []ContentItem{{
				Type: ContentTypeResource,
				Resource: &ResourceContent{
					URI: "file:///report.txt",
				},
			}},
			err: "resource content is empty",
		},
		{
			name:  "valid text",
			items: []ContentItem{{Type: ContentTypeText, Text: "ok"}},
		},
		{
			name: "valid mixed",
			items: []ContentItem{{
				Type:     ContentTypeImage,
				MIMEType: "image/png",
				Data:     "ZmFrZQ==",
			}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateContentItems(tc.items)
			if tc.err != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
		})
	}
}
