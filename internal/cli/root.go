// Package cli wires up the pinchy command tree using Cobra.
//
// One file per subcommand keeps the Cobra glue close to the behaviour it
// describes. The root command itself only assembles children and exposes
// the binary version.
package cli

import (
	"github.com/spf13/cobra"
)

// Version is set by NewRoot and read by the version subcommand.
var Version = "dev"

// NewRoot constructs the top-level pinchy command. The version string is
// supplied by main and propagated to the version subcommand.
func NewRoot(version string) *cobra.Command {
	Version = version
	root := &cobra.Command{
		Use:           "pinchy",
		Short:         "Manage containerised agent development environments",
		Long:          "Pinchy creates and manages isolated, labelled pairs of containers (an opencode agent + a dedicated rootless dockerd) that act as disposable development environments.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}
	root.AddCommand(
		newInitCmd(),
		newCreateCmd(),
		newLsCmd(),
		newSessionCmd(),
		newShellCmd(),
		newExecCmd(),
		newLogsCmd(),
		newRmCmd(),
		newStartCmd(),
		newStopCmd(),
		newRestartCmd(),
		newProxyCmd(),
		newVersionCmd(),
	)
	return root
}
