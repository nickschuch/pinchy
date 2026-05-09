package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/dockerx"
)

func newInitCmd() *cobra.Command {
	var (
		proxyImage    string
		healthTimeout time.Duration
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
reachable through the proxy.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			cli, err := dockerx.NewClient()
			if err != nil {
				return fmt.Errorf("connecting to docker: %w", err)
			}
			defer cli.Close()

			proxyRef := resolveImage(proxyImage, "PINCHY_PROXY_IMAGE", DefaultProxyImage)

			fmt.Fprintf(cmd.OutOrStdout(), "Ensuring shared network...\n")
			if err := ensureSharedInfra(ctx, cli, proxyRef, Version, healthTimeout); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Proxy ready — http://<env>.pinchy.localhost:{8080,4096,3000}\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&proxyImage, "proxy-image", "", "override the proxy image reference")
	cmd.Flags().DurationVar(&healthTimeout, "health-timeout", 60*time.Second, "max time to wait for the proxy to become healthy")
	return cmd
}
