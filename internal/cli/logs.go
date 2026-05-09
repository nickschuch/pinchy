package cli

import (
	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/dockerx"
	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

func newLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Stream logs from every container in an environment",
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
			containers, err := dockerx.ContainersForEnv(ctx, cli, name)
			if err != nil {
				return err
			}
			streams := make([]dockerx.LogStream, 0, len(containers))
			for _, c := range containers {
				role := c.Labels[pinchyenv.LabelRole]
				if role == "" {
					role = "unknown"
				}
				streams = append(streams, dockerx.LogStream{ContainerID: c.ID, Prefix: role})
			}
			return dockerx.MultiLogs(ctx, cli, streams, follow, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")
	return cmd
}
