package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"golang.org/x/term"
	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/brand"
	"github.com/nickschuch/pinchy/internal/dockerx"
	"github.com/nickschuch/pinchy/internal/env"
	pinchytable "github.com/nickschuch/pinchy/internal/table"
)

// status severity levels — higher is worse.
const (
	sevUnknown   = 0
	sevGood      = 1
	sevStarting  = 2
	sevPaused    = 3
	sevBad       = 4
	sevUnhealthy = 5
)

// containerSev maps a Docker status string (e.g. "Up 3 minutes", "Exited (0)
// 5s ago") to a severity level.
func containerSev(status string) int {
	if status == "" {
		return sevUnknown
	}
	switch strings.ToLower(strings.Fields(status)[0]) {
	case "up", "running":
		return sevGood
	case "created", "restarting", "starting":
		return sevStarting
	case "paused":
		return sevPaused
	case "exited", "stopped", "dead", "removing":
		return sevBad
	default:
		return sevStarting
	}
}

// healthSev maps a Docker health string to a severity level.
func healthSev(health string) int {
	switch health {
	case "healthy":
		return sevGood
	case "starting":
		return sevStarting
	case "unhealthy":
		return sevUnhealthy
	default:
		return sevUnknown
	}
}

// resolveStatus converts status and health strings to a human label and a
// lipgloss style. This is shared between environment rows and service rows.
func resolveStatus(status, health string) (label string, style lipgloss.Style) {
	agentSev := containerSev(status)
	healthS := healthSev(health)

	switch {
	case agentSev == sevBad:
		return "stopped", lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	case agentSev == sevPaused:
		return "paused", lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	case healthS == sevUnhealthy:
		return "unhealthy", lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	case agentSev == sevStarting || healthS == sevStarting:
		return "starting", lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	case agentSev == sevGood:
		return "running", lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	default:
		return "unknown", lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	}
}

// statusLabel resolves the worst-case status across agent, docker, and health
// for the given environment and returns a label and style.
func statusLabel(e env.Environment) (label string, style lipgloss.Style) {
	agentSev := containerSev(e.AgentStatus)
	dockerSev := containerSev(e.DockerStatus)
	healthS := healthSev(e.DockerHealth)

	switch {
	case agentSev == sevBad || dockerSev == sevBad:
		return "stopped", lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	case agentSev == sevPaused || dockerSev == sevPaused:
		return "paused", lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	case healthS == sevUnhealthy:
		return "unhealthy", lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	case agentSev == sevStarting || dockerSev == sevStarting || healthS == sevStarting:
		return "starting", lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	case agentSev == sevGood && dockerSev == sevGood:
		return "running", lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	default:
		return "unknown", lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	}
}

// lsOutput is the JSON shape for --json output.
type lsOutput struct {
	Environments []env.Environment `json:"environments"`
	Services     []env.Service     `json:"services"`
}

func newLsCmd() *cobra.Command {
	var (
		jsonOut bool
		quiet   bool
	)
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List pinchy environments",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cli, err := dockerx.NewClient()
			if err != nil {
				return err
			}
			defer cli.Close()

			envs, err := dockerx.ListEnvs(ctx, cli)
			if err != nil {
				return err
			}

			proxySvc, proxyFound, err := dockerx.ProxyStatus(ctx, cli)
			if err != nil {
				return err
			}

			consoleSvc, consoleFound, err := dockerx.ConsoleStatus(ctx, cli)
			if err != nil {
				return err
			}

			llmproxySvc, llmproxyFound, err := dockerx.LLMProxyStatus(ctx, cli)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()

			if quiet {
				for _, e := range envs {
					fmt.Fprintln(out, e.Name)
				}
				return nil
			}

			if jsonOut {
				services := []env.Service{}
				if proxyFound {
					services = append(services, proxySvc)
				}
				if consoleFound {
					services = append(services, consoleSvc)
				}
				if llmproxyFound {
					services = append(services, llmproxySvc)
				}
				return json.NewEncoder(out).Encode(lsOutput{
					Environments: envs,
					Services:     services,
				})
			}

		// Detect whether stdout is a real terminal; suppress color when piped.
		isTTY := term.IsTerminal(int(os.Stdout.Fd()))

		// renderHeading returns a bold, uppercased, accent-colored heading
		// matching fang's section title style when writing to a TTY, or plain
		// text when piped so escape codes don't pollute output.
		renderHeading := func(text string) string {
			if !isTTY {
				return text
			}
			return lipgloss.NewStyle().
				Bold(true).
				Transform(strings.ToUpper).
				Foreground(lipgloss.Color(brand.Accent)).
				Render(text)
		}

		// Environments table.
			envHeaders := []string{"NAME", "STATUS", "WORKDIR", "CREATED"}
			envRows := make([][]string, 0, len(envs))
			for _, e := range envs {
				created := ""
				if !e.Created.IsZero() {
					created = e.Created.Format("2006-01-02 15:04")
				}
				label, style := statusLabel(e)
				status := label
				if isTTY {
					status = style.Render(label)
				}
				workdirCell := e.Workdir
				if e.WorktreeBranch != "" {
					workdirCell = e.Workdir + " [worktree: " + e.WorktreeBranch + "]"
				}
				envRows = append(envRows, []string{
					e.Name,
					status,
					workdirCell,
					created,
				})
			}
		fmt.Fprintln(out, renderHeading("ENVIRONMENTS"))
		if err := pinchytable.Print(out, envHeaders, envRows); err != nil {
			return err
		}

		// Services table — only rendered when at least one service is known.
		if !proxyFound && !consoleFound && !llmproxyFound {
			return nil
		}

		fmt.Fprintln(out, renderHeading("SERVICES"))
			svcHeaders := []string{"NAME", "STATUS", "PORTS", "URL", "CREATED"}
			svcRows := make([][]string, 0, 2)

			addSvcRow := func(svc env.Service, url string) {
				created := ""
				if !svc.Created.IsZero() {
					created = svc.Created.Format("2006-01-02 15:04")
				}
				ports := make([]string, 0, len(svc.Ports))
				for _, p := range svc.Ports {
					ports = append(ports, fmt.Sprintf("%d", p))
				}
				label, style := resolveStatus(svc.Status, svc.Health)
				statusStr := label
				if isTTY {
					statusStr = style.Render(label)
				}
				svcRows = append(svcRows, []string{
					svc.Name,
					statusStr,
					strings.Join(ports, ", "),
					url,
					created,
				})
			}

			if proxyFound {
				addSvcRow(proxySvc, "")
			}
			if consoleFound {
				addSvcRow(consoleSvc, "http://console.pinchy.localhost:8080")
			}
			if llmproxyFound {
				addSvcRow(llmproxySvc, "http://llmproxy.pinchy.localhost:8080 (admin UI)")
			}

			return pinchytable.Print(out, svcHeaders, svcRows)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "only print environment names")
	return cmd
}
