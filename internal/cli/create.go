package cli

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nickschuch/pinchy/internal/config"
	"github.com/nickschuch/pinchy/internal/dockerx"
	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

func newCreateCmd() *cobra.Command {
	var (
		workdir       string
		noAttach      bool
		agentImage    string
		dockerImage   string
		proxyImage    string
		healthTimeout time.Duration
		configPath    string
		envFlags      []string
		envFiles      []string
	)
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create and start a new environment",
		Long: `Create and start a new pinchy environment with the given name.

The current directory is bind-mounted into the agent at /data unless
--workdir is supplied. By default, opens an opencode TUI session on success.

Environment variables are injected into the agent container using the
following precedence (later sources win for the same key):

  1. Baseline (DOCKER_HOST — always set by pinchy)
  2. Global "env:" block in the config file
  3. Per-environment "environments.<name>.env:" block in the config file
  4. --env-file files (in order given)
  5. -e / --env flags (in order given)

Config file is resolved from (first match wins):
  --config flag > $PINCHY_CONFIG env var > $XDG_CONFIG_HOME/pinchy/config.yaml
  (falls back to ~/.config/pinchy/config.yaml)

Environment variable values are never printed; only their names are shown.

NOTE: environment variables are baked into the container at create time.
Changes to the config file take effect only after recreating the
environment (pinchy rm <name> && pinchy create <name>).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := pinchyenv.ValidateName(name); err != nil {
				return err
			}
			ctx := cmd.Context()

			cli, err := dockerx.NewClient()
			if err != nil {
				return fmt.Errorf("connecting to docker: %w", err)
			}
			defer cli.Close()

			// Refuse to clobber an existing environment.
			if existing, ok, err := dockerx.ResolveEnv(ctx, cli, name); err != nil {
				return err
			} else if ok {
				return fmt.Errorf("environment %q already exists (agent: %s)", name, existing.AgentStatus)
			}

			agentRef := resolveImage(agentImage, "PINCHY_AGENT_IMAGE", DefaultAgentImage)
			dockerRef := resolveImage(dockerImage, "PINCHY_DOCKER_IMAGE", DefaultDockerImage)
			proxyRef := resolveImage(proxyImage, "PINCHY_PROXY_IMAGE", DefaultProxyImage)

			if err := dockerx.EnsureImageLocal(ctx, cli, agentRef); err != nil {
				return err
			}
			if err := dockerx.EnsureImageLocal(ctx, cli, dockerRef); err != nil {
				return err
			}

			// Ensure the shared proxy infrastructure is up before creating the env.
			fmt.Fprintf(cmd.OutOrStdout(), "Ensuring shared proxy infrastructure...\n")
			if err := ensureSharedInfra(ctx, cli, proxyRef, Version, healthTimeout); err != nil {
				return err
			}

			absWorkdir, err := resolveWorkdir(workdir)
			if err != nil {
				return err
			}
			if fi, err := os.Stat(absWorkdir); err != nil {
				return fmt.Errorf("workdir %q: %w", absWorkdir, err)
			} else if !fi.IsDir() {
				return fmt.Errorf("workdir %q is not a directory", absWorkdir)
			}

			// Build the injected env map using the defined precedence.
			injectedEnv, err := buildEnvMap(name, configPath, envFiles, envFlags)
			if err != nil {
				return err
			}
			if len(injectedEnv) > 0 {
				keys := sortedKeys(injectedEnv)
				fmt.Fprintf(cmd.OutOrStdout(), "Injecting environment variables: %s\n", strings.Join(keys, ", "))
			}

			created := time.Now().UTC().Format(time.RFC3339)

			// Volumes + network — created first so containers can attach.
			netName := pinchyenv.NetworkName(name)
			dataVol := pinchyenv.DataVolumeName(name)
			sockVol := pinchyenv.SockVolumeName(name)
			netLabels := pinchyenv.BaseLabels(name, "network", Version, created)
			dataLabels := pinchyenv.BaseLabels(name, "volume-data", Version, created)
			sockLabels := pinchyenv.BaseLabels(name, "volume-sock", Version, created)

			if err := dockerx.EnsureNetwork(ctx, cli, netName, netLabels); err != nil {
				return err
			}
			if err := dockerx.EnsureVolume(ctx, cli, dataVol, dataLabels); err != nil {
				return err
			}
			if err := dockerx.EnsureVolume(ctx, cli, sockVol, sockLabels); err != nil {
				return err
			}

			// Docker daemon container first so the agent can rely on a
			// reachable socket from the moment it starts.
			dockerLabels := pinchyenv.BaseLabels(name, pinchyenv.RoleDocker, Version, created)
			// Merge Traefik labels so the proxy treats the dind container as a
			// second backend server for each service, enabling failover routing.
			for k, v := range pinchyenv.ProxyTraefikLabelsDocker(name) {
				dockerLabels[k] = v
			}
			dockerCfg, dockerHost := dockerx.DockerConfig(dockerx.DockerSpec{
				Image:      dockerRef,
				DataVolume: dataVol,
				SockVolume: sockVol,
				Network:    netName,
				Labels:     dockerLabels,
			})
			fmt.Fprintf(cmd.OutOrStdout(), "Starting docker daemon...\n")
			dockerID, err := dockerx.CreateAndStart(ctx, cli, pinchyenv.DockerContainerName(name), dockerCfg, dockerHost, netName)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Waiting for docker daemon to become healthy (timeout %s)...\n", healthTimeout)
			waitCtx, cancel := waitContext(ctx, healthTimeout)
			defer cancel()
			if err := dockerx.WaitForHealthy(waitCtx, cli, pinchyenv.DockerContainerName(name), 2*time.Second); err != nil {
				return fmt.Errorf("docker daemon never became healthy: %w", err)
			}
			// Connect the dind container to the shared network so that
			// port-published services inside it are reachable by Traefik.
			if err := dockerx.ConnectToSharedNetwork(ctx, cli, dockerID); err != nil {
				return fmt.Errorf("connecting docker daemon to shared network: %w", err)
			}

			// Agent container.
			agentLabels := pinchyenv.BaseLabels(name, pinchyenv.RoleAgent, Version, created)
			agentLabels[pinchyenv.LabelWorkdir] = absWorkdir
			// Merge Traefik routing labels so the proxy auto-discovers this agent.
			// The agent declares routers, services, and active healthchecks;
			// the dind container (above) contributes a second server per service.
			for k, v := range pinchyenv.ProxyTraefikLabelsAgent(name) {
				agentLabels[k] = v
			}
			agentCfg, agentHost := dockerx.AgentConfig(dockerx.AgentSpec{
				Image:       agentRef,
				HostWorkdir: absWorkdir,
				SockVolume:  sockVol,
				Network:     netName,
				Labels:      agentLabels,
				Env:         injectedEnv,
			})
			fmt.Fprintf(cmd.OutOrStdout(), "Starting agent...\n")
			agentID, err := dockerx.CreateAndStart(ctx, cli, pinchyenv.AgentContainerName(name), agentCfg, agentHost, netName)
			if err != nil {
				return err
			}
			// Connect the agent to the shared network so Traefik can reach it.
			if err := dockerx.ConnectToSharedNetwork(ctx, cli, agentID); err != nil {
				return fmt.Errorf("connecting agent to shared network: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Environment %q is ready.\n", name)
			if noAttach {
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Attaching TUI — detach with Ctrl-c or Ctrl-d.\n")
			return runDockerExecTTYEnv(ctx, pinchyenv.AgentContainerName(name),
				[]string{"XDG_DATA_HOME=/tmp/opencode-tui"},
				[]string{"opencode", "attach", "http://localhost:4096"},
			)
		},
	}
	cmd.Flags().StringVar(&workdir, "workdir", "", "host directory to bind-mount at /data (defaults to $PWD)")
	cmd.Flags().BoolVar(&noAttach, "no-attach", false, "do not attach to the agent after creation")
	cmd.Flags().StringVar(&agentImage, "agent-image", "", "override the agent image reference")
	cmd.Flags().StringVar(&dockerImage, "docker-image", "", "override the docker image reference")
	cmd.Flags().StringVar(&proxyImage, "proxy-image", "", "override the proxy image reference")
	cmd.Flags().DurationVar(&healthTimeout, "health-timeout", 2*time.Minute, "max time to wait for the docker daemon to become healthy")
	cmd.Flags().StringVar(&configPath, "config", "", "path to pinchy config file (overrides $PINCHY_CONFIG and XDG default)")
	cmd.Flags().StringArrayVarP(&envFlags, "env", "e", nil, "inject environment variable into the agent (KEY=VALUE); repeatable; takes precedence over config file")
	cmd.Flags().StringArrayVar(&envFiles, "env-file", nil, "file of KEY=VALUE pairs to inject into the agent; repeatable; takes precedence over config file")
	return cmd
}

// buildEnvMap assembles the final map of user-supplied environment variables
// to inject into the agent container, applying the documented precedence:
//
//  1. Global env from config file
//  2. Per-environment env from config file
//  3. --env-file files (in order)
//  4. -e/--env flags (in order)
func buildEnvMap(envName, configPath string, envFiles, envFlags []string) (map[string]string, error) {
	out := make(map[string]string)

	// 1 & 2: config file (global then per-env, via EnvFor).
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	for k, v := range cfg.EnvFor(envName) {
		out[k] = v
	}

	// 3: --env-file files.
	for _, path := range envFiles {
		pairs, err := parseEnvFile(path)
		if err != nil {
			return nil, fmt.Errorf("--env-file %q: %w", path, err)
		}
		for k, v := range pairs {
			out[k] = v
		}
	}

	// 4: -e/--env flags.
	for _, kv := range envFlags {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, fmt.Errorf("--env %q: expected KEY=VALUE format", kv)
		}
		if k == "" {
			return nil, fmt.Errorf("--env %q: key must not be empty", kv)
		}
		out[k] = v
	}

	return out, nil
}

// parseEnvFile reads a file of KEY=VALUE pairs, ignoring blank lines and
// lines starting with '#'.
func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := make(map[string]string)
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE format, got %q", lineNum, line)
		}
		if k == "" {
			return nil, fmt.Errorf("line %d: key must not be empty", lineNum)
		}
		out[k] = v
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// sortedKeys returns the keys of m sorted alphabetically.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
