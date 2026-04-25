package tokenizer

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageTokenTiling(t *testing.T) {
	require.Equal(t, gpt5ImageBaseTokens+gpt5ImageTileTokens*4, gpt5ImageTokens(512, 512))
	require.Equal(t, gpt5ImageBaseTokens+gpt5ImageTileTokens*12, gpt5ImageTokens(2048, 512))
}

func TestImageURLRequiresHTTPS(t *testing.T) {
	counter, err := newCounter(ModelGPT5)
	require.NoError(t, err)

	_, _, err = counter.imageDimensions(context.Background(), "http://example.com/image.png")
	require.Error(t, err)
	require.ErrorContains(t, err, "https")
}

func TestCountImageTokensHTTPS(t *testing.T) {
	imageBytes := pngBytes(t, 1, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imageBytes)
	}))
	defer server.Close()

	counter, err := newCounter(ModelGPT5)
	require.NoError(t, err)
	counter.httpClient = server.Client()

	tokens, err := counter.countImageTokens(context.Background(), ImageContent{ImageURL: server.URL, Detail: ImageDetailHigh})
	require.NoError(t, err)
	require.Equal(t, gpt5ImageBaseTokens+gpt5ImageTileTokens*4, tokens)
}

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}
