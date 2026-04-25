package tokenizer

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/pkoukk/tiktoken-go"
)

const (
	bpePathEnv     = "TOKEN_COUNTING_BPE_PATH"
	bpeDirEnv      = "TOKEN_COUNTING_BPE_DIR"
	bpeFileName    = "o200k_base.tiktoken"
	defaultBpeHint = "provide a local o200k_base.tiktoken via TOKEN_COUNTING_BPE_PATH or TOKEN_COUNTING_BPE_DIR"
)

var bpeLoaderOnce sync.Once

func configureBpeLoader() {
	bpeLoaderOnce.Do(func() {
		if strings.TrimSpace(os.Getenv(bpePathEnv)) == "" && strings.TrimSpace(os.Getenv(bpeDirEnv)) == "" {
			return
		}
		tiktoken.SetBpeLoader(&localBpeLoader{})
	})
}

type localBpeLoader struct{}

func (l *localBpeLoader) LoadTiktokenBpe(path string) (map[string]int, error) {
	localPath, err := resolveBpePath(path)
	if err != nil {
		return nil, err
	}
	contents, err := os.ReadFile(localPath)
	if err != nil {
		return nil, fmt.Errorf("read bpe file: %w", err)
	}
	return parseBpeContents(contents)
}

func resolveBpePath(input string) (string, error) {
	if path := strings.TrimSpace(os.Getenv(bpePathEnv)); path != "" {
		return path, nil
	}
	if dir := strings.TrimSpace(os.Getenv(bpeDirEnv)); dir != "" {
		return filepath.Join(dir, bpeFileName), nil
	}
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", errors.New(defaultBpeHint)
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return "", errors.New(defaultBpeHint)
	}
	return trimmed, nil
}

func parseBpeContents(contents []byte) (map[string]int, error) {
	lines := strings.Split(string(contents), "\n")
	bpeRanks := make(map[string]int, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, " ")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid bpe line: %q", line)
		}
		token, err := base64.StdEncoding.DecodeString(parts[0])
		if err != nil {
			return nil, err
		}
		rank, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, err
		}
		bpeRanks[string(token)] = rank
	}
	return bpeRanks, nil
}
