package cli

import (
	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/console"
	"github.com/nickschuch/pinchy/internal/dockerx"
)

func newConsoleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "console",
		Short: "Manage the shared discovery console",
	}
	cmd.AddCommand(newConsoleServeCmd(), newConsoleLogsCmd())
	return cmd
}

// newConsoleServeCmd is the entry point used inside the console container.
// It is hidden from top-level help because it is not a user-facing command.
func newConsoleServeCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:    "serve",
		Short:  "Start the console HTTP server (runs inside the console container)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return console.Serve(cmd.Context(), addr)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", ":8080", "address to listen on")
	return cmd
}

func newConsoleLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Fetch logs from the shared console container",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			cli, err := dockerx.NewClient()
			if err != nil {
				return err
			}
			defer cli.Close()

			return dockerx.ConsoleLogs(ctx, cli, follow, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "stream logs until interrupted")
	return cmd
}


