package tokenizer

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCountTokensUsesO200kBase(t *testing.T) {
	msg := Message{Item: MessageItem{Role: RoleUser, Content: []ContentPart{{
		Type: ContentTypeInputText,
		Text: "hallo world!",
	}}}}

	tokens, err := CountTokens(context.Background(), ModelGPT5, []Message{msg})
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.Equal(t, int32(gpt5MessageOverheadTokens+4), tokens[0])
}

func TestCountTokensRejectsAudioFile(t *testing.T) {
	data := base64.StdEncoding.EncodeToString([]byte("audio"))
	msg := Message{Item: MessageItem{Role: RoleUser, Content: []ContentPart{{
		Type: ContentTypeInputFile,
		File: FileContent{Filename: "clip.mp3", Data: data},
	}}}}

	_, err := CountTokens(context.Background(), ModelGPT5, []Message{msg})
	require.Error(t, err)
	require.ErrorContains(t, err, "audio files are not supported")
}
