package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/agynio/agn-cli/internal/config"
	"github.com/agynio/agn-cli/internal/llm"
	"github.com/agynio/agn-cli/internal/loop"
	"github.com/agynio/agn-cli/internal/mcp"
	"github.com/agynio/agn-cli/internal/message"
	"github.com/agynio/agn-cli/internal/server"
	"github.com/agynio/agn-cli/internal/state"
	"github.com/agynio/agn-cli/internal/summarize"
)

const mcpCommandEnv = "AGN_MCP_COMMAND"

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
	var conversationID string

	cmd := &cobra.Command{
		Use:   "exec <prompt>",
		Short: "Run a single prompt and exit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt := strings.TrimSpace(args[0])
			if prompt == "" {
				return errors.New("prompt is required")
			}
			cfg, err := config.LoadDefault()
			if err != nil {
				return err
			}
			agent, cleanup, err := buildAgent(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer cleanup()
			result, err := agent.Run(cmd.Context(), loop.Input{
				Prompt:         message.NewHumanMessage(prompt),
				ConversationID: strings.TrimSpace(conversationID),
			})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), result.Response)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.ErrOrStderr(), "conversation_id: %s\n", result.ConversationID)
			return err
		},
	}
	cmd.Flags().StringVar(&conversationID, "conversation-id", "", "Conversation ID to resume")
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
			agent, cleanup, err := buildAgent(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer cleanup()
			srv := server.New(agent)
			return srv.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	return cmd
}

func buildAgent(ctx context.Context, cfg config.Config) (*loop.Agent, func(), error) {
	apiKey, err := cfg.LLM.Auth.ResolveAPIKey()
	if err != nil {
		return nil, func() {}, err
	}
	llmClient, err := llm.NewClient(cfg.LLM.Endpoint, apiKey, cfg.LLM.Model)
	if err != nil {
		return nil, func() {}, err
	}
	summarizer, err := summarize.New(llmClient, summarize.Config{})
	if err != nil {
		return nil, func() {}, err
	}
	store, err := state.NewDefaultLocalStore()
	if err != nil {
		return nil, func() {}, err
	}
	mcpClient, err := newMCPClient(ctx)
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() {
		if mcpClient != nil {
			_ = mcpClient.Close()
		}
	}
	agent, err := loop.NewAgent(loop.AgentConfig{
		Store:        store,
		LLM:          llmClient,
		Summarizer:   summarizer,
		MCP:          mcpClient,
		SystemPrompt: cfg.SystemPrompt,
	})
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return agent, cleanup, nil
}

func newMCPClient(ctx context.Context) (*mcp.Client, error) {
	commandLine := strings.TrimSpace(os.Getenv(mcpCommandEnv))
	if commandLine == "" {
		return nil, nil
	}
	fields := strings.Fields(commandLine)
	if len(fields) == 0 {
		return nil, nil
	}
	client, err := mcp.NewProcessClient(ctx, fields[0], fields[1:]...)
	if err != nil {
		return nil, err
	}
	return client, nil
}
