package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/dockerx"
	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

func newRmCmd() *cobra.Command {
	var (
		force        bool
		keepVolumes  bool
	)
	cmd := &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove an environment",
		Long:  "Stop and remove all containers, networks, and (by default) volumes belonging to the named environment.",
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
			env, err := dockerx.MustExist(ctx, cli, name)
			if err != nil {
				return err
			}
			if env.AgentRunning && !force {
				fmt.Fprintf(cmd.OutOrStdout(), "Environment %q is running. Remove anyway? [y/N] ", name)
				reader := bufio.NewReader(os.Stdin)
				line, _ := reader.ReadString('\n')
				if !strings.EqualFold(strings.TrimSpace(line), "y") {
					return fmt.Errorf("aborted")
				}
			}

			// Force-remove containers (this stops them too).
			if err := dockerx.RemoveContainer(ctx, cli, pinchyenv.AgentContainerName(name), true); err != nil {
				return err
			}
			if err := dockerx.RemoveContainer(ctx, cli, pinchyenv.DockerContainerName(name), true); err != nil {
				return err
			}
			if err := dockerx.RemoveNetwork(ctx, cli, pinchyenv.NetworkName(name)); err != nil {
				return err
			}
			if !keepVolumes {
				if err := dockerx.RemoveVolume(ctx, cli, pinchyenv.DataVolumeName(name)); err != nil {
					return err
				}
				if err := dockerx.RemoveVolume(ctx, cli, pinchyenv.SockVolumeName(name)); err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed environment %q.\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "do not prompt before removing a running environment")
	cmd.Flags().BoolVar(&keepVolumes, "keep-volumes", false, "preserve named volumes (image cache and socket dir)")
	return cmd
}
