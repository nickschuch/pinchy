package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/dockerx"
	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

func newStartCmd() *cobra.Command {
	var (
		healthTimeout time.Duration
		proxyImage    string
	)
	cmd := &cobra.Command{
		Use:   "start <name>",
		Short: "Start a previously-stopped environment",
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

			proxyRef := resolveImage(proxyImage, "PINCHY_PROXY_IMAGE", DefaultProxyImage)
			if err := ensureSharedInfra(ctx, cli, proxyRef, Version, healthTimeout); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Starting docker daemon...\n")
			if err := dockerx.StartContainer(ctx, cli, pinchyenv.DockerContainerName(name)); err != nil {
				return err
			}
			waitCtx, cancel := waitContext(ctx, healthTimeout)
			defer cancel()
			if err := dockerx.WaitForHealthy(waitCtx, cli, pinchyenv.DockerContainerName(name), 2*time.Second); err != nil {
				return fmt.Errorf("docker daemon never became healthy: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Starting agent...\n")
			if err := dockerx.StartContainer(ctx, cli, pinchyenv.AgentContainerName(name)); err != nil {
				return err
			}
			// Re-attach both containers to the shared network in case they were
			// disconnected (e.g. after a manual network prune).
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
				if !dockerx.HasTraefikLabels(cmd.Context(), cli, e.DockerContainerID) {
					fmt.Fprintf(cmd.OutOrStdout(), "note: this environment lacks dind routing labels; recreate with 'pinchy rm %s && pinchy create %s' for failover routing\n", name, name)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Environment %q started.\n", name)
			return nil
		},
	}
	cmd.Flags().DurationVar(&healthTimeout, "health-timeout", 2*time.Minute, "max time to wait for the docker daemon to become healthy")
	cmd.Flags().StringVar(&proxyImage, "proxy-image", "", "override the proxy image reference")
	return cmd
}
