package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/dockerx"
	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

// newOpenCmd returns the `pinchy open <name>` command, which prints the
// OpenCode web UI URL for a running environment and opens it in the user's
// default browser.
func newOpenCmd() *cobra.Command {
	var (
		noBrowser     bool
		healthTimeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "open <name>",
		Short: "Open the OpenCode web UI for a running environment",
		Long: `Open the OpenCode web UI for an existing pinchy environment.

Waits for the agent's web server to become ready (useful immediately after
'pinchy start'), prints the URL, and opens it in your default browser unless
--no-browser is supplied.`,
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

			agentWaitCtx, agentCancel := waitContext(ctx, healthTimeout)
			defer agentCancel()
			if err := dockerx.WaitForAgentReady(agentWaitCtx, cli, pinchyenv.AgentContainerName(name), 2*time.Second,
				func(f string, a ...any) { fmt.Fprintf(cmd.OutOrStdout(), f, a...) },
			); err != nil {
				return fmt.Errorf("agent never became ready: %w", err)
			}

			printWebURL(cmd.OutOrStdout(), name)
			if !noBrowser {
				if err := openInBrowser(ctx, envWebURL(name)); err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "Warning: could not open browser: %v\n", err)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the OpenCode web UI URL but do not open a browser")
	cmd.Flags().DurationVar(&healthTimeout, "health-timeout", 30*time.Second, "max time to wait for the agent to become ready")
	return cmd
}

// newSessionCmd returns a hidden deprecated alias for `pinchy open`.
// It prints a deprecation notice then delegates to the open handler so
// existing scripts that relied on `pinchy session` keep working for one
// release.
func newSessionCmd() *cobra.Command {
	open := newOpenCmd()
	open.Use = "session <name>"
	open.Short = "Deprecated: use 'pinchy open <name>' instead"
	open.Long = "Deprecated: use 'pinchy open <name>' instead.\n\n" + open.Long
	open.Hidden = true
	origRunE := open.RunE
	open.RunE = func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(cmd.ErrOrStderr(), "Note: 'pinchy session' is deprecated; use 'pinchy open' instead.")
		return origRunE(cmd, args)
	}
	return open
}
