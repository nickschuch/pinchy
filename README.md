# Pinchy

Pinchy is a CLI for managing containerised development environments, each
consisting of an [opencode](https://opencode.ai) agent backed by a dedicated
rootless Docker-in-Docker daemon. Environments are discovered via Docker labels
— pinchy holds no on-disk state of its own.

> Pinchy is early-stage; expect rough edges.

## What you get

- An **agent container** running `opencode web` on port 4096 — accessible via
  browser or TUI and reachable through the shared Traefik proxy at
  `http://<env>.pinchy.localhost:4096/`.
- A **private dind container** (rootless Docker-in-Docker) per environment,
  wired to the agent over a shared socket volume. Run `docker compose` inside
  the agent and it talks to its own daemon.
- A **shared Traefik proxy** that routes `<env>.pinchy.localhost:{8080,4096,3000}`
  to the right agent. Active healthchecks on each backend mean compose-based
  services (reachable via the dind container) and in-agent processes both work
  transparently.
- Label-driven discovery — no database, no config files, no daemons beyond
  Docker itself.

## Architecture

```
  host
  ──────────────────────────────────────────────────────
  :8080  :4096  :3000          pinchy-proxy (Traefik)
                 │
                 │  Host(<env>.pinchy.localhost)
                 ▼
        ╔════════════════════╗
        ║  pinchy-shared     ║  (bridge network)
        ╚════════════════════╝
               │          │
               ▼          ▼
    ┌──────────────┐  ┌──────────────┐
    │  agent       │  │  docker      │
    │              │  │  (dind-      │
    │  opencode    │  │  rootless)   │
    │  web :4096   │  │              │
    └──────┬───────┘  └──────┬───────┘
           │                 │
           └────── sock ─────┘
                  volume
```

Both the agent and dind containers are also on a private per-environment bridge
(`pinchy-<env>`) so they can reach each other by container name.

## Quick start

### Prerequisites

- Linux with Docker (rootless recommended; rootful works)
- Go toolchain matching `go.mod` (managed via `mise` — see `mise.toml`)
- `make`

### Build

```
make build       # produces ./bin/pinchy
make images      # build agent, docker, and proxy images locally
```

Run `./bin/pinchy` directly, or alias it for convenience (e.g.
`alias pinchy="$PWD/bin/pinchy"`). A proper install path will come later.

Override image references at build time or via environment variables:

```
AGENT_IMAGE=myregistry/my-agent:dev make images
```

### Create an environment

```
pinchy create example
```

Pinchy will:

1. Ensure the shared network and Traefik proxy are running (`pinchy init`
   handles this automatically).
2. Start a dind container and wait for it to become healthy.
3. Start the agent container (opencode web on `0.0.0.0:4096`).
4. Connect both containers to `pinchy-shared` so the proxy can route to them.
5. Open a TUI session.

### Access it

| Method | Address |
|--------|---------|
| Browser | `http://example.pinchy.localhost:4096/` |
| TUI (resume last session) | `pinchy session example` |
| TUI (fresh session) | `pinchy session example --new` |
| Shell | `pinchy shell example` |

To detach from the TUI, press **Ctrl-c**.

## Commands

| Command | Description |
|---------|-------------|
| `pinchy init` | Initialise shared proxy infrastructure |
| `pinchy create <name>` | Create and start a new environment |
| `pinchy ls` | List environments and proxy status |
| `pinchy session <name>` | Open an opencode TUI session in the agent |
| `pinchy shell <name>` | Open an interactive shell in a service container |
| `pinchy exec <name> -- <cmd>` | Run a one-off command in a service container |
| `pinchy logs <name>` | Stream logs from every container in an environment |
| `pinchy start <name>` | Start a stopped environment |
| `pinchy stop <name>` | Stop a running environment |
| `pinchy restart <name>` | Stop and start an environment |
| `pinchy rm <name>` | Remove an environment and its volumes |
| `pinchy proxy logs` | Fetch logs from the shared proxy container |
| `pinchy version` | Print the pinchy version |

Run `pinchy <command> --help` for flags and full descriptions.

## Configuration

Pinchy reads an optional config file for injecting environment variables into
agent containers. The file is resolved in this order (first match wins):

1. `--config <path>` flag
2. `$PINCHY_CONFIG` environment variable
3. `$XDG_CONFIG_HOME/pinchy/config.yaml` (default: `~/.config/pinchy/config.yaml`)

A missing file is not an error.

### Config file format

```yaml
# Variables injected into every agent container.
env:
  ANTHROPIC_API_KEY: sk-ant-...

# Per-environment overrides (merged on top of the global env block).
environments:
  example:
    env:
      SOME_VAR: override-value
```

### Env injection precedence

Later sources win for the same key:

1. Global `env:` block
2. Per-environment `env:` block
3. `--env-file` files (in order)
4. `-e` / `--env` flags

### Authentication

To protect the opencode web server, set `OPENCODE_SERVER_PASSWORD` before
creating the environment:

```
pinchy create example -e OPENCODE_SERVER_PASSWORD=secret
```

Or add it to the config file under `env:`.

## How it works

### Container topology

Each environment consists of two containers:

| Container | Image | Role |
|-----------|-------|------|
| `pinchy-<env>-agent` | `ghcr.io/nickschuch/pinchy-agent` | opencode web server + tooling |
| `pinchy-<env>-docker` | `ghcr.io/nickschuch/pinchy-docker` | rootless dind daemon |

Plus one global shared service:

| Container | Image | Role |
|-----------|-------|------|
| `pinchy-proxy` | `ghcr.io/nickschuch/pinchy-proxy` | Traefik reverse proxy |

### Routing

The proxy is configured with three Traefik entrypoints (`:8080`, `:4096`,
`:3000`). Every agent container carries Traefik labels that declare:

- A router: `Host(\`<env>.pinchy.localhost\`)` on each entrypoint.
- A service with two backend servers: the agent (for in-agent processes) and
  the dind container (for compose-published services).
- Active healthchecks (`/healthz` on `:8080`/`:3000`, `/global/health` on
  `:4096`) so Traefik only routes to a backend that is actually listening.

Failover is automatic: run `docker compose up` inside the agent and the proxy
starts sending `:8080` traffic to the dind container within one healthcheck
cycle (~5 s).

### State and volumes

Pinchy creates three Docker resources per environment:

| Resource | Name | Purpose |
|----------|------|---------|
| Volume | `pinchy-<env>-data` | dind image store (persists across stop/start) |
| Volume | `pinchy-<env>-sock` | shared docker socket (agent ↔ dind) |
| Network | `pinchy-<env>` | private per-environment bridge |

All resources are labelled `pinchy.managed=true` and `pinchy.env=<env>`.
`pinchy rm <name>` removes all of them. `pinchy ls` discovers them by label
query — no external state is required.

## Examples

The `examples/http-server/` directory contains a minimal Go HTTP server with a
multi-stage Dockerfile and a `docker-compose.yaml`. Run it inside any pinchy
agent environment to verify routing:

```
cd examples/http-server
docker compose up --build

# from the host:
curl http://example.pinchy.localhost:8080/
curl http://example.pinchy.localhost:8080/healthz
```

See `examples/http-server/AGENTS.md` for agent-specific context and conventions.

## Development

### Repo layout

```
cmd/pinchy/         binary entrypoint
internal/cli/       cobra command implementations
internal/dockerx/   Docker SDK helpers (container, network, volume, proxy)
internal/env/       label constants, name helpers, environment model
internal/config/    config file loading and validation
internal/table/     table rendering helper
images/agent/       agent Dockerfile (opencode + dev tooling)
images/docker/      dind-rootless Dockerfile
images/proxy/       Traefik Dockerfile
examples/           sample workloads
```

### Tests

```
make test
```

### Image override

All three image references can be overridden at runtime:

```
PINCHY_AGENT_IMAGE=myregistry/agent:dev pinchy create example
PINCHY_DOCKER_IMAGE=myregistry/docker:dev pinchy create example
PINCHY_PROXY_IMAGE=myregistry/proxy:dev pinchy create example
```
