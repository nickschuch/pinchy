package cli

import (
	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/dockerx"
)

func newProxyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Manage the shared proxy service",
	}
	cmd.AddCommand(newProxyLogsCmd())
	return cmd
}

func newProxyLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Fetch logs from the shared proxy container",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			cli, err := dockerx.NewClient()
			if err != nil {
				return err
			}
			defer cli.Close()

			return dockerx.ProxyLogs(ctx, cli, follow, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "stream logs until interrupted")
	return cmd
}
