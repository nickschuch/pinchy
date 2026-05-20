package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/config"
	"github.com/nickschuch/pinchy/internal/dockerx"
)

func newInitCmd() *cobra.Command {
	var (
		proxyImage    string
		consoleImage  string
		llmproxyImage string
		healthTimeout time.Duration
		recreate      bool
		configPath    string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialise shared proxy infrastructure",
		Long: `Ensure the shared pinchy-shared bridge network and Traefik reverse-proxy
container exist and are healthy.

Run this once before creating environments, or rely on 'pinchy create' which
calls init automatically. The command is idempotent — safe to run repeatedly.

The proxy listens on host ports 8080, 4096, and 3000 and routes traffic for
<env>.pinchy.localhost to the matching agent container on the same port.

Dev servers inside the agent must bind 0.0.0.0 (not 127.0.0.1) to be
reachable through the proxy.

If llm_proxy.anthropic_api_key is set in the config file, a shared LiteLLM
proxy container (pinchy-llmproxy) is also started. Agents are pre-wired to
use it; they never receive the real Anthropic API key.

Use --recreate to force the proxy, console, and llmproxy containers to be
removed and recreated from scratch. The shared network is not affected.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			cli, err := dockerx.NewClient()
			if err != nil {
				return fmt.Errorf("connecting to docker: %w", err)
			}
			defer cli.Close()

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			proxyRef := resolveImage(proxyImage, "PINCHY_PROXY_IMAGE", DefaultProxyImage)
			consoleRef := resolveImage(consoleImage, "PINCHY_CONSOLE_IMAGE", DefaultConsoleImage)
			llmproxyRef := resolveImage(llmproxyImage, "PINCHY_LLMPROXY_IMAGE", DefaultLLMProxyImage)

			fmt.Fprintf(cmd.OutOrStdout(), "Ensuring shared network...\n")
			if err := ensureSharedInfra(ctx, cli, proxyRef, consoleRef, llmproxyRef, Version, healthTimeout, recreate, cfg.LLMProxy); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Proxy ready   — http://<env>.pinchy.localhost:{8080,4096,3000}\n")
			fmt.Fprintf(cmd.OutOrStdout(), "Console ready — http://console.pinchy.localhost:8080\n")
			if cfg.LLMProxyEnabled() {
				fmt.Fprintf(cmd.OutOrStdout(), "LLM proxy ready — http://pinchy-llmproxy:%s/anthropic/v1 (internal)\n", "4000")
				fmt.Fprintf(cmd.OutOrStdout(), "LLM proxy UI    — http://llmproxy.pinchy.localhost:8080\n")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "LLM proxy not configured — set llm_proxy.anthropic_api_key in config to enable\n")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&proxyImage, "proxy-image", "", "override the proxy image reference")
	cmd.Flags().StringVar(&consoleImage, "console-image", "", "override the console image reference")
	cmd.Flags().StringVar(&llmproxyImage, "llmproxy-image", "", "override the llmproxy image reference")
	cmd.Flags().DurationVar(&healthTimeout, "health-timeout", 60*time.Second, "max time to wait for the proxy to become healthy")
	cmd.Flags().BoolVar(&recreate, "recreate", false, "remove and recreate the shared proxy, console, and llmproxy containers")
	cmd.Flags().StringVar(&configPath, "config", "", "path to pinchy config file (overrides $PINCHY_CONFIG and XDG default)")
	return cmd
}
