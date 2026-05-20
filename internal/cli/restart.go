package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/config"
	"github.com/nickschuch/pinchy/internal/dockerx"
	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

func newRestartCmd() *cobra.Command {
	var (
		healthTimeout time.Duration
		proxyImage    string
		consoleImage  string
		llmproxyImage string
		recreate      bool
		configPath    string
		noBrowser     bool
	)
	cmd := &cobra.Command{
		Use:   "restart <name>",
		Short: "Stop and start an environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := pinchyenv.ValidateName(name); err != nil {
				return err
			}
			ctx := cmd.Context()
			cli, err := dockerx.NewClient()
			if err != nil {
				return err
			}
			defer cli.Close()
			if _, err := dockerx.MustExist(ctx, cli, name); err != nil {
				return err
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			proxyRef := resolveImage(proxyImage, "PINCHY_PROXY_IMAGE", DefaultProxyImage)
			consoleRef := resolveImage(consoleImage, "PINCHY_CONSOLE_IMAGE", DefaultConsoleImage)
			llmproxyRef := resolveImage(llmproxyImage, "PINCHY_LLMPROXY_IMAGE", DefaultLLMProxyImage)
			if err := ensureSharedInfra(ctx, cli, proxyRef, consoleRef, llmproxyRef, Version, healthTimeout, recreate, cfg.LLMProxy); err != nil {
				return err
			}

			if err := dockerx.StopContainer(ctx, cli, pinchyenv.AgentContainerName(name)); err != nil {
				return err
			}
			if err := dockerx.StopContainer(ctx, cli, pinchyenv.DockerContainerName(name)); err != nil {
				return err
			}
			if err := dockerx.StartContainer(ctx, cli, pinchyenv.DockerContainerName(name)); err != nil {
				return err
			}
			waitCtx, cancel := waitContext(ctx, healthTimeout)
			defer cancel()
			if err := dockerx.WaitForHealthy(waitCtx, cli, pinchyenv.DockerContainerName(name), 2*time.Second); err != nil {
				return fmt.Errorf("docker daemon never became healthy: %w", err)
			}
			if err := dockerx.StartContainer(ctx, cli, pinchyenv.AgentContainerName(name)); err != nil {
				return err
			}
		// Re-attach both containers to the shared network.
		e, _, _ := dockerx.ResolveEnv(ctx, cli, name)
		if e.AgentContainerID != "" {
			if err := dockerx.ConnectToSharedNetwork(ctx, cli, e.AgentContainerID); err != nil {
				return fmt.Errorf("connecting agent to shared network: %w", err)
			}
		}
		if e.DockerContainerID != "" {
			if err := dockerx.ConnectToSharedNetwork(ctx, cli, e.DockerContainerID); err != nil {
				return fmt.Errorf("connecting docker daemon to shared network: %w", err)
			}
			// Warn if the dind container pre-dates the dual-backend routing
			// labels; failover will not work until the environment is recreated.
			if !dockerx.HasTraefikLabels(ctx, cli, e.DockerContainerID) {
				fmt.Fprintf(cmd.OutOrStdout(), "note: this environment lacks dind routing labels; recreate with 'pinchy rm %s && pinchy create %s' for failover routing\n", name, name)
			}
		}

		// Wait for the opencode web server to be ready before returning so
		// that a subsequent 'pinchy session <name>' does not race.
		fmt.Fprintf(cmd.OutOrStdout(), "Waiting for agent to become ready (timeout %s)...\n", healthTimeout)
		agentWaitCtx, agentCancel := waitContext(ctx, healthTimeout)
		defer agentCancel()
		if err := dockerx.WaitForAgentReady(agentWaitCtx, cli, pinchyenv.AgentContainerName(name), 2*time.Second,
			func(f string, a ...any) { fmt.Fprintf(cmd.OutOrStdout(), f, a...) },
		); err != nil {
			return fmt.Errorf("agent never became ready: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Environment %q restarted.\n", name)
		printWebURL(cmd.OutOrStdout(), name)
		if !noBrowser {
			if err := openInBrowser(ctx, envWebURL(name)); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Warning: could not open browser: %v\n", err)
			}
		}
		return nil
		},
	}
	cmd.Flags().DurationVar(&healthTimeout, "health-timeout", 2*time.Minute, "max time to wait for the docker daemon to become healthy")
	cmd.Flags().StringVar(&proxyImage, "proxy-image", "", "override the proxy image reference")
	cmd.Flags().StringVar(&consoleImage, "console-image", "", "override the console image reference")
	cmd.Flags().StringVar(&llmproxyImage, "llmproxy-image", "", "override the llmproxy image reference")
	cmd.Flags().BoolVar(&recreate, "recreate", false, "remove and recreate the shared proxy, console, and llmproxy containers")
	cmd.Flags().StringVar(&configPath, "config", "", "path to pinchy config file (overrides $PINCHY_CONFIG and XDG default)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the OpenCode web UI URL but do not open a browser")
	return cmd
}
