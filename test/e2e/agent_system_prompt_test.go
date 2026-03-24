//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	systemPromptExpectation = "You are personal assistant"
	userPromptExpectation   = "hi"
	assistantExpectation    = "Hello! I am here to help!"
)

func TestAgentSystemPrompt(t *testing.T) {
	server := newSystemPromptServer(t)
	defer server.Close()

	env := newTestEnvWithSystemPrompt(t, server.URL, systemPromptExpectation)
	binary := buildAgnBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stdout, _ := runAgnWithContext(t, ctx, binary, env.env, "exec", userPromptExpectation)
	if strings.TrimSpace(stdout) != assistantExpectation {
		t.Fatalf("expected response %q, got %q", assistantExpectation, stdout)
	}
}

func newSystemPromptServer(t *testing.T) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req responseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if !matchesSystemPrompt(req, systemPromptExpectation) {
			http.Error(w, "system prompt missing or incorrect", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(lastUserPrompt(req.Input)) != userPromptExpectation {
			http.Error(w, "user prompt missing or incorrect", http.StatusBadRequest)
			return
		}

		response := map[string]any{
			"id": "resp-id",
			"output": []any{
				map[string]any{
					"type": "message",
					"role": "assistant",
					"content": []any{
						map[string]any{"type": "output_text", "text": assistantExpectation},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	return httptest.NewServer(handler)
}

func matchesSystemPrompt(req responseRequest, expected string) bool {
	if strings.TrimSpace(req.Instructions) != "" {
		return strings.TrimSpace(req.Instructions) == expected
	}
	return systemPromptFromInput(req.Input) == expected
}

func systemPromptFromInput(input json.RawMessage) string {
	var messages []map[string]any
	if err := json.Unmarshal(input, &messages); err != nil {
		return ""
	}
	for _, item := range messages {
		role, _ := item["role"].(string)
		if role != "developer" && role != "system" {
			continue
		}
		if text := extractInputText(item["content"]); text != "" {
			return text
		}
	}
	return ""
}
