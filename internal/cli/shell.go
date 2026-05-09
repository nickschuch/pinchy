package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/dockerx"
	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

func newShellCmd() *cobra.Command {
	var service string
	cmd := &cobra.Command{
		Use:   "shell <name>",
		Short: "Open an interactive shell in a service container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
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
			// Try bash first (agent has it); fall back to sh.
			shells := []string{"bash", "sh"}
			var lastErr error
			for _, sh := range shells {
				if err := runDockerExecTTY(ctx, containerName, []string{sh}); err != nil {
					lastErr = err
					continue
				}
				return nil
			}
			return lastErr
		},
	}
	addServiceFlag(cmd, &service)
	return cmd
}
