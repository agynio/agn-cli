package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/agynio/agn-cli/internal/config"
	"github.com/agynio/agn-cli/internal/llm"
	"github.com/agynio/agn-cli/internal/loop"
	"github.com/agynio/agn-cli/internal/mcp"
	"github.com/agynio/agn-cli/internal/message"
	"github.com/agynio/agn-cli/internal/server"
	"github.com/agynio/agn-cli/internal/state"
	"github.com/agynio/agn-cli/internal/summarize"
	"github.com/agynio/agn-cli/internal/telemetry"
)

func main() {
	root := &cobra.Command{
		Use:          "agn",
		Short:        "Agent loop CLI",
		SilenceUsage: true,
	}
	root.AddCommand(execCommand(), serveCommand())
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func execCommand() *cobra.Command {
	var threadID string
	cmd := &cobra.Command{
		Use:   "exec <prompt>",
		Short: "Run a single prompt and exit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt := strings.TrimSpace(args[0])
			if prompt == "" {
				return errors.New("prompt is required")
			}
			trimmedThreadID := strings.TrimSpace(threadID)
			cfg, err := config.LoadDefault()
			if err != nil {
				return err
			}
			agent, _, cleanup, err := buildAgent(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer cleanup()
			result, err := agent.Run(cmd.Context(), loop.Input{
				ThreadID: trimmedThreadID,
				Prompt:   message.NewHumanMessage(prompt),
			})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "thread_id: %s\n", result.ThreadID); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), result.Response)
			return err
		},
	}
	cmd.Flags().StringVar(&threadID, "thread-id", "", "Thread ID to resume")
	cmd.AddCommand(execResumeCommand())
	return cmd
}

func execResumeCommand() *cobra.Command {
	var useLast bool
	cmd := &cobra.Command{
		Use:   "resume <thread-id> <prompt>",
		Short: "Resume a thread with a follow-up prompt",
		Args: func(cmd *cobra.Command, args []string) error {
			if useLast {
				if len(args) != 1 {
					return errors.New("prompt is required when --last is set")
				}
				return nil
			}
			if len(args) != 2 {
				return errors.New("thread ID and prompt are required")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadDefault()
			if err != nil {
				return err
			}
			agent, store, cleanup, err := buildAgent(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer cleanup()

			var threadID string
			var prompt string
			if useLast {
				threads, err := store.List(cmd.Context())
				if err != nil {
					return err
				}
				if len(threads) == 0 {
					return errors.New("no threads found")
				}
				threadID = threads[0].ID
				prompt = strings.TrimSpace(args[0])
			} else {
				threadID = strings.TrimSpace(args[0])
				prompt = strings.TrimSpace(args[1])
			}
			if threadID == "" {
				return errors.New("thread ID is required")
			}
			if prompt == "" {
				return errors.New("prompt is required")
			}
			result, err := agent.Run(cmd.Context(), loop.Input{
				ThreadID: threadID,
				Prompt:   message.NewHumanMessage(prompt),
			})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "thread_id: %s\n", result.ThreadID); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), result.Response)
			return err
		},
	}
	cmd.Flags().BoolVar(&useLast, "last", false, "Use most recent thread")
	return cmd
}

func serveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run JSON-RPC server over stdin/stdout",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadDefault()
			if err != nil {
				return err
			}
			agent, store, cleanup, err := buildAgent(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer cleanup()
			srv := server.New(agent, store)
			return srv.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	return cmd
}

func buildAgent(ctx context.Context, cfg config.Config) (*loop.Agent, state.Store, func(), error) {
	tracerProvider, err := telemetry.Init(ctx)
	if err != nil {
		return nil, nil, func() {}, err
	}
	tracer := tracerProvider.Tracer("agn")
	var mcpProvider mcp.ToolProvider
	cleanup := func() {
		if mcpProvider != nil {
			_ = mcpProvider.Close()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tracerProvider.Shutdown(shutdownCtx)
	}

	apiKey, err := cfg.LLM.Auth.ResolveAPIKey()
	if err != nil {
		cleanup()
		return nil, nil, func() {}, err
	}
	llmClient, err := llm.NewClient(cfg.LLM.Endpoint, apiKey, cfg.LLM.Model)
	if err != nil {
		cleanup()
		return nil, nil, func() {}, err
	}
	summarizerClient := llmClient
	if cfg.Summarization.LLM != nil {
		summarizerKey, err := cfg.Summarization.LLM.Auth.ResolveAPIKey()
		if err != nil {
			cleanup()
			return nil, nil, func() {}, err
		}
		summarizerClient, err = llm.NewClient(
			cfg.Summarization.LLM.Endpoint,
			summarizerKey,
			cfg.Summarization.LLM.Model,
		)
		if err != nil {
			cleanup()
			return nil, nil, func() {}, err
		}
	}
	summarizer, err := summarize.New(summarizerClient, summarize.Config{
		KeepTokens: cfg.Summarization.KeepTokens,
		MaxTokens:  cfg.Summarization.MaxTokens,
	})
	if err != nil {
		cleanup()
		return nil, nil, func() {}, err
	}
	store, err := state.NewDefaultLocalStore()
	if err != nil {
		cleanup()
		return nil, nil, func() {}, err
	}
	mcpProvider, err = newMCPProvider(ctx, cfg.MCP)
	if err != nil {
		cleanup()
		return nil, nil, func() {}, err
	}
	agent, err := loop.NewAgent(loop.AgentConfig{
		Store:        store,
		LLM:          llmClient,
		Summarizer:   summarizer,
		MCP:          mcpProvider,
		SystemPrompt: cfg.SystemPrompt,
		Tracer:       tracer,
	})
	if err != nil {
		cleanup()
		return nil, nil, func() {}, err
	}
	return agent, store, cleanup, nil
}

func newMCPProvider(ctx context.Context, cfg config.MCPConfig) (mcp.ToolProvider, error) {
	if len(cfg.Servers) == 0 {
		return nil, nil
	}
	providers := make([]mcp.ToolProvider, 0, len(cfg.Servers))
	for name, serverCfg := range cfg.Servers {
		var provider mcp.ToolProvider
		var err error
		if serverCfg.Command != "" {
			env := os.Environ()
			for key, value := range serverCfg.Env {
				env = append(env, key+"="+value)
			}
			provider, err = mcp.NewProcessClientWithEnv(ctx, serverCfg.Command, serverCfg.Args, env)
		} else {
			provider, err = mcp.NewHTTPClient(ctx, serverCfg.URL)
		}
		if err != nil {
			for _, existing := range providers {
				_ = existing.Close()
			}
			return nil, fmt.Errorf("start MCP server %q: %w", name, err)
		}
		providers = append(providers, provider)
	}
	return mcp.NewMultiClient(providers)
}
