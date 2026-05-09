package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/dockerx"
	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

func newExecCmd() *cobra.Command {
	var (
		service string
		tty     bool
	)
	cmd := &cobra.Command{
		Use:                   "exec <name> [--service agent|docker] [--tty] -- <cmd> [args...]",
		Short:                 "Run a one-off command in a service container",
		Args:                  cobra.MinimumNArgs(2),
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			rest := args[1:]
			if err := pinchyenv.ValidateName(name); err != nil {
				return err
			}
			containerName := pinchyenv.ContainerNameFor(name, service)
			if containerName == "" {
				return fmt.Errorf("unknown --service %q (expected agent or docker)", service)
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
			if tty {
				return runDockerExecTTY(ctx, containerName, rest)
			}
			return runDockerExecPlain(ctx, containerName, rest)
		},
	}
	addServiceFlag(cmd, &service)
	cmd.Flags().BoolVar(&tty, "tty", false, "allocate a TTY for the exec session")
	return cmd
}
