package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/dockerx"
	pinchyenv "github.com/nickschuch/pinchy/internal/env"
	"github.com/nickschuch/pinchy/internal/gitx"
)

func newRmCmd() *cobra.Command {
	var (
		force         bool
		keepVolumes   bool
		keepWorktree  bool
	)
	cmd := &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove an environment",
		Long: `Stop and remove all containers, networks, and (by default) volumes belonging to the named environment.

When the environment was created with automatic git worktree support, the
worktree directory and its branch are also removed by default. Pass
--keep-worktree to preserve them (e.g. to inspect or push the branch first).`,
		Args: cobra.ExactArgs(1),
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

			// Capture worktree metadata before removing the containers (which
			// hold the labels we need).
			worktreeRepo := env.WorktreeRepo
			worktreeBranch := env.WorktreeBranch
			worktreePath := env.Workdir // LabelWorkdir points at the worktree dir

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

			// Clean up git worktree if applicable.
			if worktreeRepo != "" && !keepWorktree {
				fmt.Fprintf(cmd.OutOrStdout(), "Removing git worktree %s...\n", worktreePath)
				if err := gitx.RemoveWorktree(worktreeRepo, worktreePath); err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "Warning: could not remove worktree: %v\n", err)
				}
				if worktreeBranch != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Deleting git branch %s...\n", worktreeBranch)
					if err := gitx.DeleteBranch(worktreeRepo, worktreeBranch); err != nil {
						fmt.Fprintf(cmd.OutOrStdout(), "Warning: could not delete branch %q: %v\n", worktreeBranch, err)
					}
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed environment %q.\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "do not prompt before removing a running environment")
	cmd.Flags().BoolVar(&keepVolumes, "keep-volumes", false, "preserve named volumes (image cache and socket dir)")
	cmd.Flags().BoolVar(&keepWorktree, "keep-worktree", false, "preserve the git worktree directory and branch (if the env was created with worktree support)")
	return cmd
}
