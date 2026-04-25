package tokenizer

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"

	_ "golang.org/x/image/webp"
)

const (
	maxImageDimension = 2048.0
	minImageDimension = 768.0
	imageTileSize     = 512.0
)

func (c *counter) countImageTokens(ctx context.Context, imageContent ImageContent) (int, error) {
	imageURL := strings.TrimSpace(imageContent.ImageURL)
	fileID := strings.TrimSpace(imageContent.FileID)
	if imageURL == "" {
		if fileID != "" {
			return 0, nil
		}
		return 0, errors.New("image_url is required")
	}
	if imageContent.Detail == ImageDetailLow {
		return gpt5ImageBaseTokens, nil
	}

	width, height, err := c.imageDimensions(ctx, imageURL)
	if err != nil {
		return 0, err
	}
	return gpt5ImageTokens(float64(width), float64(height)), nil
}

func (c *counter) imageDimensions(ctx context.Context, imageURL string) (int, int, error) {
	trimmed := strings.TrimSpace(imageURL)
	if strings.HasPrefix(trimmed, "data:") {
		data, err := decodeDataURL(trimmed)
		if err != nil {
			return 0, 0, err
		}
		cfg, err := decodeImageConfig(bytes.NewReader(data))
		if err != nil {
			return 0, 0, err
		}
		return cfg.Width, cfg.Height, nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return 0, 0, fmt.Errorf("parse image_url: %w", err)
	}
	if parsed.Scheme != "https" {
		return 0, 0, fmt.Errorf("image_url must use https, got %q", parsed.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return 0, 0, fmt.Errorf("build image request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("fetch image_url: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, 0, fmt.Errorf("fetch image_url: status %s", resp.Status)
	}
	cfg, err := decodeImageConfig(resp.Body)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

func decodeDataURL(value string) ([]byte, error) {
	comma := strings.Index(value, ",")
	if comma < 0 {
		return nil, errors.New("invalid data url")
	}
	meta := value[len("data:"):comma]
	data := value[comma+1:]
	if !strings.Contains(meta, ";base64") {
		return nil, errors.New("data url is not base64 encoded")
	}
	decoded, err := decodeBase64(data)
	if err != nil {
		return nil, fmt.Errorf("decode data url: %w", err)
	}
	return decoded, nil
}

func decodeImageConfig(reader io.Reader) (image.Config, error) {
	cfg, _, err := image.DecodeConfig(reader)
	if err != nil {
		return image.Config{}, fmt.Errorf("decode image config: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return image.Config{}, errors.New("invalid image dimensions")
	}
	return cfg, nil
}

func decodeBase64(data string) ([]byte, error) {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return nil, errors.New("base64 data is empty")
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err == nil {
		return decoded, nil
	}
	decoded, rawErr := base64.RawStdEncoding.DecodeString(trimmed)
	if rawErr != nil {
		return nil, err
	}
	return decoded, nil
}

func gpt5ImageTokens(width, height float64) int {
	w := width
	h := height
	if w <= 0 || h <= 0 {
		panic("invalid image dimensions")
	}
	if w > maxImageDimension || h > maxImageDimension {
		scale := math.Min(maxImageDimension/w, maxImageDimension/h)
		w *= scale
		h *= scale
	}
	shortest := math.Min(w, h)
	if shortest > 0 {
		scale := minImageDimension / shortest
		w *= scale
		h *= scale
	}
	widthTiles := math.Ceil(w / imageTileSize)
	heightTiles := math.Ceil(h / imageTileSize)
	tiles := int(widthTiles * heightTiles)
	return gpt5ImageBaseTokens + gpt5ImageTileTokens*tiles
}
