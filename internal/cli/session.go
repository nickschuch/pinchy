package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/dockerx"
	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

// sessionTUIDataDir is the XDG_DATA_HOME redirect for the TUI client process.
// Without this, the TUI's SQLite initialisation conflicts with the WAL lock
// held by the opencode web server (which uses ~/.local/share/opencode).
const sessionTUIDataDir = "/tmp/opencode-tui"

// sessionShellWrapper resumes the most recent session (--continue) on second
// and subsequent attaches, but skips --continue on the very first attach to
// avoid an "Invalid session ID 'dummy'" error toast in the TUI when there is
// no prior client state.
//
// The presence of $XDG_DATA_HOME/opencode is used as the marker: it is
// created the first time the TUI initialises its local SQLite store.
const sessionShellWrapper = `if [ -d "$XDG_DATA_HOME/opencode" ]; then exec opencode attach http://localhost:4096 --continue; else exec opencode attach http://localhost:4096; fi`

func newSessionCmd() *cobra.Command {
	var newSession bool
	cmd := &cobra.Command{
		Use:   "session <name>",
		Short: "Open an opencode TUI session in the agent",
		Long: `Open an opencode TUI connected to the in-container opencode web server.

By default, session resumes the most recent session so detaching with Ctrl-c
and reattaching later drops you back where you left off. Pass --new to skip
the resume and start with no active session selected.

The TUI shares sessions and state with any browser clients connected to
http://<name>.pinchy.localhost:4096/. Detach with Ctrl-c or Ctrl-d.`,
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
			if !env.AgentRunning {
				return fmt.Errorf("environment %q is not running (agent: %s); start it with `pinchy start %s`", name, env.AgentStatus, name)
			}

			var execArgs []string
			if newSession {
				execArgs = []string{"opencode", "attach", "http://localhost:4096"}
			} else {
				execArgs = []string{"sh", "-c", sessionShellWrapper}
			}
			return runDockerExecTTYEnv(ctx, pinchyenv.AgentContainerName(name),
				[]string{"XDG_DATA_HOME=" + sessionTUIDataDir},
				execArgs,
			)
		},
	}
	cmd.Flags().BoolVar(&newSession, "new", false, "start with no active session instead of resuming the most recent one")
	return cmd
}
