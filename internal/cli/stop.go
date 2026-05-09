package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/dockerx"
	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop a running environment without removing it",
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
			// Stop the agent first; the docker daemon is its dependency.
			if err := dockerx.StopContainer(ctx, cli, pinchyenv.AgentContainerName(name)); err != nil {
				return err
			}
			if err := dockerx.StopContainer(ctx, cli, pinchyenv.DockerContainerName(name)); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Environment %q stopped.\n", name)
			return nil
		},
	}
}
