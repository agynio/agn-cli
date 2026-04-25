package tokenizer

import (
	"context"
	"fmt"
	"path"
	"strings"
)

var textExtensions = map[string]struct{}{
	".txt":      {},
	".md":       {},
	".markdown": {},
	".csv":      {},
	".tsv":      {},
	".json":     {},
	".yaml":     {},
	".yml":      {},
	".xml":      {},
	".html":     {},
	".htm":      {},
	".js":       {},
	".ts":       {},
	".jsx":      {},
	".tsx":      {},
	".go":       {},
	".py":       {},
	".rb":       {},
	".java":     {},
	".kt":       {},
	".kts":      {},
	".rs":       {},
	".c":        {},
	".cc":       {},
	".cpp":      {},
	".h":        {},
	".hpp":      {},
	".css":      {},
	".scss":     {},
	".toml":     {},
	".ini":      {},
	".cfg":      {},
	".conf":     {},
	".sql":      {},
	".sh":       {},
	".bash":     {},
	".zsh":      {},
	".ps1":      {},
	".log":      {},
	".env":      {},
}

var audioExtensions = map[string]struct{}{
	".aac":  {},
	".flac": {},
	".mp3":  {},
	".mpeg": {},
	".m4a":  {},
	".ogg":  {},
	".wav":  {},
}

func (c *counter) countFileTokens(ctx context.Context, file FileContent) (int, error) {
	data, err := decodeBase64(file.Data)
	if err != nil {
		return 0, fmt.Errorf("decode file data: %w", err)
	}

	filename := strings.TrimSpace(file.Filename)
	if filename == "" {
		return 0, fmt.Errorf("filename is required")
	}
	fileExt := strings.ToLower(path.Ext(filename))
	if fileExt == "" {
		return 0, fmt.Errorf("unsupported file extension for %q", filename)
	}

	if fileExt == ".pdf" {
		return c.countPDFTokens(data)
	}
	if _, ok := audioExtensions[fileExt]; ok {
		return 0, fmt.Errorf("audio files are not supported: %s", fileExt)
	}
	if _, ok := textExtensions[fileExt]; ok {
		return c.countText(string(data)), nil
	}

	return 0, fmt.Errorf("unsupported file extension %q", fileExt)
}
