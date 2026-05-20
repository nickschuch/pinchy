package dockerx

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	pinchyenv "github.com/nickschuch/pinchy/internal/env"
)

// listManaged returns every container created by pinchy, regardless of state.
func listManaged(ctx context.Context, cli *client.Client, envName string) ([]container.Summary, error) {
	args := filters.NewArgs()
	args.Add("label", pinchyenv.LabelManaged+"="+pinchyenv.ManagedTrue)
	if envName != "" {
		args.Add("label", pinchyenv.LabelEnv+"="+envName)
	}
	return cli.ContainerList(ctx, container.ListOptions{All: true, Filters: args})
}

// ContainersForEnv returns all pinchy-managed containers belonging to envName.
func ContainersForEnv(ctx context.Context, cli *client.Client, envName string) ([]container.Summary, error) {
	return listManaged(ctx, cli, envName)
}

// ResolveEnv collects information about a single environment by name, using
// only label queries. It returns ("", false, nil) if the environment does not
// exist (no containers found).
func ResolveEnv(ctx context.Context, cli *client.Client, envName string) (pinchyenv.Environment, bool, error) {
	containers, err := listManaged(ctx, cli, envName)
	if err != nil {
		return pinchyenv.Environment{}, false, err
	}
	if len(containers) == 0 {
		return pinchyenv.Environment{}, false, nil
	}
	out := pinchyenv.Environment{Name: envName}
	for _, c := range containers {
		role := c.Labels[pinchyenv.LabelRole]
		switch role {
		case pinchyenv.RoleAgent:
			out.AgentContainerID = c.ID
			out.AgentStatus = c.Status
			out.AgentRunning = c.State == "running"
			out.Workdir = c.Labels[pinchyenv.LabelWorkdir]
			out.WorktreeRepo = c.Labels[pinchyenv.LabelWorktreeRepo]
			out.WorktreeBranch = c.Labels[pinchyenv.LabelWorktreeBranch]
			out.Version = c.Labels[pinchyenv.LabelVersion]
			if t, err := time.Parse(time.RFC3339, c.Labels[pinchyenv.LabelCreated]); err == nil {
				out.Created = t
			}
		case pinchyenv.RoleDocker:
			out.DockerContainerID = c.ID
			out.DockerStatus = c.Status
			// Health is reported in the status string as e.g.
			// "Up 2 minutes (healthy)" but for the structured form we need an
			// inspect call; defer to caller if they want it.
		}
	}
	return out, true, nil
}

// ListEnvs returns all pinchy environments on the host, sorted by name.
func ListEnvs(ctx context.Context, cli *client.Client) ([]pinchyenv.Environment, error) {
	containers, err := listManaged(ctx, cli, "")
	if err != nil {
		return nil, err
	}
	byName := map[string]*pinchyenv.Environment{}
	for _, c := range containers {
		name := c.Labels[pinchyenv.LabelEnv]
		if name == "" {
			continue
		}
		e, ok := byName[name]
		if !ok {
			e = &pinchyenv.Environment{Name: name}
			byName[name] = e
		}
		role := c.Labels[pinchyenv.LabelRole]
		switch role {
		case pinchyenv.RoleAgent:
			e.AgentContainerID = c.ID
			e.AgentStatus = c.Status
			e.AgentRunning = c.State == "running"
			e.Workdir = c.Labels[pinchyenv.LabelWorkdir]
			e.WorktreeRepo = c.Labels[pinchyenv.LabelWorktreeRepo]
			e.WorktreeBranch = c.Labels[pinchyenv.LabelWorktreeBranch]
			e.Version = c.Labels[pinchyenv.LabelVersion]
			if t, err := time.Parse(time.RFC3339, c.Labels[pinchyenv.LabelCreated]); err == nil {
				e.Created = t
			}
		case pinchyenv.RoleDocker:
			e.DockerContainerID = c.ID
			e.DockerStatus = c.Status
		}
	}
	// Optionally enrich with health for docker containers.
	for _, e := range byName {
		if e.DockerContainerID == "" {
			continue
		}
		insp, err := cli.ContainerInspect(ctx, e.DockerContainerID)
		if err != nil {
			continue
		}
		if insp.State != nil && insp.State.Health != nil {
			e.DockerHealth = insp.State.Health.Status
		}
	}
	out := make([]pinchyenv.Environment, 0, len(byName))
	for _, e := range byName {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// MustExist returns the environment by name or an error suitable for CLI
// presentation when it is missing.
func MustExist(ctx context.Context, cli *client.Client, envName string) (pinchyenv.Environment, error) {
	e, ok, err := ResolveEnv(ctx, cli, envName)
	if err != nil {
		return e, err
	}
	if !ok {
		return e, fmt.Errorf("environment %q not found", envName)
	}
	return e, nil
}
