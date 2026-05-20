package cli

import (
	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/dockerx"
)

func newLLMProxyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "llmproxy",
		Short: "Manage the shared LLM proxy service",
	}
	cmd.AddCommand(newLLMProxyLogsCmd())
	return cmd
}

func newLLMProxyLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Fetch logs from the shared LLM proxy container",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			cli, err := dockerx.NewClient()
			if err != nil {
				return err
			}
			defer cli.Close()

			return dockerx.LLMProxyLogs(ctx, cli, follow, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "stream logs until interrupted")
	return cmd
}
