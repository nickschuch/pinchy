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
	"github.com/nickschuch/pinchy/internal/gitx"
)

func newCreateCmd() *cobra.Command {
	var (
		workdir       string
		noWorktree    bool
		noBrowser     bool
		agentImage    string
		dockerImage   string
		proxyImage    string
		consoleImage  string
		llmproxyImage string
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
--workdir is supplied. On success the OpenCode web UI URL is printed and
opened in your default browser (pass --no-browser to skip the browser open).

Git worktree support (automatic):

  When --workdir (or the current directory) is inside a git repository,
  pinchy automatically creates a new git worktree at:

    <repo>/.pinchy-worktrees/<name>/

  on a new branch also named <name> (branched from the current HEAD). That
  worktree directory is then bind-mounted into the agent at /data instead of
  the source repository. This gives each environment its own isolated branch
  while sharing the repo's git history and objects.

  Requirements:
    - git must be on your PATH
    - a branch named <name> must not already exist in the repo

  Pass --no-worktree to skip this behaviour and use a plain bind-mount.

  On 'pinchy rm', the worktree directory and branch are removed automatically.
  Use 'pinchy rm --keep-worktree' to preserve them.

  NOTE: .pinchy-worktrees/ will appear as an untracked directory in git
  status. Add it to .gitignore or .git/info/exclude to suppress the noise.

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

			// Load config early so we can pass LLMProxy config to ensureSharedInfra.
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			agentRef := resolveImage(agentImage, "PINCHY_AGENT_IMAGE", DefaultAgentImage)
			dockerRef := resolveImage(dockerImage, "PINCHY_DOCKER_IMAGE", DefaultDockerImage)
			proxyRef := resolveImage(proxyImage, "PINCHY_PROXY_IMAGE", DefaultProxyImage)
			consoleRef := resolveImage(consoleImage, "PINCHY_CONSOLE_IMAGE", DefaultConsoleImage)
			llmproxyRef := resolveImage(llmproxyImage, "PINCHY_LLMPROXY_IMAGE", DefaultLLMProxyImage)

			if err := dockerx.EnsureImageLocal(ctx, cli, agentRef); err != nil {
				return err
			}
			if err := dockerx.EnsureImageLocal(ctx, cli, dockerRef); err != nil {
				return err
			}

			// Ensure the shared proxy infrastructure is up before creating the env.
			fmt.Fprintf(cmd.OutOrStdout(), "Ensuring shared proxy infrastructure...\n")
			if err := ensureSharedInfra(ctx, cli, proxyRef, consoleRef, llmproxyRef, Version, healthTimeout, false, cfg.LLMProxy); err != nil {
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

			// Git worktree auto-detect: if --workdir resolves inside a git
			// repo and --no-worktree is not set, create a dedicated worktree
			// for this environment and use it as the bind-mount path.
			var worktreeRepo, worktreeBranch string
			if !noWorktree {
				absWorkdir, worktreeRepo, worktreeBranch, err = maybeCreateWorktree(
					cmd, absWorkdir, name,
				)
				if err != nil {
					return err
				}
			}

			// If a worktree was created and something fails later, clean it up.
			worktreeCreated := worktreeRepo != ""
			defer func() {
				if worktreeCreated {
					worktreePath := gitx.WorktreePath(worktreeRepo, name)
					_ = gitx.RemoveWorktree(worktreeRepo, worktreePath)
					_ = gitx.DeleteBranch(worktreeRepo, worktreeBranch)
				}
			}()

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
			if worktreeRepo != "" {
				agentLabels[pinchyenv.LabelWorktreeRepo] = worktreeRepo
				agentLabels[pinchyenv.LabelWorktreeBranch] = worktreeBranch
			}
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

		// Wait for the opencode web server inside the agent to become ready
		// before printing the URL and opening the browser.
		fmt.Fprintf(cmd.OutOrStdout(), "Waiting for agent to become ready (timeout %s)...\n", healthTimeout)
		agentWaitCtx, agentCancel := waitContext(ctx, healthTimeout)
		defer agentCancel()
		if err := dockerx.WaitForAgentReady(agentWaitCtx, cli, pinchyenv.AgentContainerName(name), 2*time.Second,
			func(f string, a ...any) { fmt.Fprintf(cmd.OutOrStdout(), f, a...) },
		); err != nil {
			return fmt.Errorf("agent never became ready: %w", err)
		}

		// Environment is fully created — disarm the worktree rollback.
		worktreeCreated = false

		fmt.Fprintf(cmd.OutOrStdout(), "Environment %q is ready.\n", name)
		printWebURL(cmd.OutOrStdout(), name)
		if !noBrowser {
			if err := openInBrowser(ctx, envWebURL(name)); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Warning: could not open browser: %v\n", err)
			}
		}
		return nil
		},
	}
	cmd.Flags().StringVar(&workdir, "workdir", "", "host directory to bind-mount at /data (defaults to $PWD)")
	cmd.Flags().BoolVar(&noWorktree, "no-worktree", false, "disable automatic git worktree creation when --workdir is inside a git repo")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the OpenCode web UI URL but do not open a browser")
	// --no-attach is a hidden deprecated alias for --no-browser.
	cmd.Flags().BoolVar(&noBrowser, "no-attach", false, "deprecated: use --no-browser")
	_ = cmd.Flags().MarkHidden("no-attach")
	cmd.Flags().StringVar(&agentImage, "agent-image", "", "override the agent image reference")
	cmd.Flags().StringVar(&dockerImage, "docker-image", "", "override the docker image reference")
	cmd.Flags().StringVar(&proxyImage, "proxy-image", "", "override the proxy image reference")
	cmd.Flags().StringVar(&consoleImage, "console-image", "", "override the console image reference")
	cmd.Flags().DurationVar(&healthTimeout, "health-timeout", 2*time.Minute, "max time to wait for the docker daemon to become healthy")
	cmd.Flags().StringVar(&configPath, "config", "", "path to pinchy config file (overrides $PINCHY_CONFIG and XDG default)")
	cmd.Flags().StringArrayVarP(&envFlags, "env", "e", nil, "inject environment variable into the agent (KEY=VALUE); repeatable; takes precedence over config file")
	cmd.Flags().StringArrayVar(&envFiles, "env-file", nil, "file of KEY=VALUE pairs to inject into the agent; repeatable; takes precedence over config file")
	cmd.Flags().StringVar(&llmproxyImage, "llmproxy-image", "", "override the llmproxy image reference")
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

// maybeCreateWorktree checks whether absWorkdir is inside a git repository
// and, if so, creates a new worktree at <repo>/.pinchy-worktrees/<envName> on
// a new branch named envName. It returns the (possibly unchanged) bind-mount
// path along with the repo root and branch name (both empty when no worktree
// was created).
//
// If git is not found on PATH the function silently falls back to a plain
// bind-mount (returns absWorkdir unchanged).
func maybeCreateWorktree(cmd *cobra.Command, absWorkdir, envName string) (bindPath, repoRoot, branch string, err error) {
	repoRoot, found, findErr := gitx.FindRepoRoot(absWorkdir)
	if findErr != nil {
		if findErr == gitx.ErrNoGit {
			// git not installed — fall back gracefully.
			fmt.Fprintf(cmd.OutOrStdout(), "Warning: git not found in PATH; skipping worktree creation.\n")
			return absWorkdir, "", "", nil
		}
		return "", "", "", fmt.Errorf("detecting git repository: %w", findErr)
	}
	if !found {
		// Not inside a git repo — plain bind-mount.
		return absWorkdir, "", "", nil
	}

	// Check the branch doesn't already exist.
	exists, err := gitx.BranchExists(repoRoot, envName)
	if err != nil {
		return "", "", "", fmt.Errorf("checking git branch: %w", err)
	}
	if exists {
		return "", "", "", fmt.Errorf(
			"git branch %q already exists in %s\n"+
				"Hint: delete it with 'git branch -D %s' or choose a different environment name.",
			envName, repoRoot, envName,
		)
	}

	worktreePath := gitx.WorktreePath(repoRoot, envName)

	// Refuse if the target directory already exists (stale leftover).
	if _, statErr := os.Stat(worktreePath); statErr == nil {
		return "", "", "", fmt.Errorf(
			"worktree directory %q already exists\n"+
				"Hint: remove it manually and run 'git worktree prune' in %s.",
			worktreePath, repoRoot,
		)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Creating git worktree at %s (branch: %s)...\n", worktreePath, envName)
	if err := gitx.AddWorktree(repoRoot, worktreePath, envName); err != nil {
		return "", "", "", fmt.Errorf("creating git worktree: %w", err)
	}

	return worktreePath, repoRoot, envName, nil
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
