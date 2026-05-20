# Pinchy

Pinchy is a CLI for managing containerised development environments, each
consisting of an [opencode](https://opencode.ai) agent backed by a dedicated
rootless Docker-in-Docker daemon. Environments are discovered via Docker labels
— pinchy holds no on-disk state of its own.

> Pinchy is early-stage; expect rough edges.

## What you get

- An **agent container** running `opencode web` on port 4096 — accessible via
  browser and reachable through the shared Traefik proxy at
  `http://<env>.pinchy.localhost:4096/`.
- A **private dind container** (rootless Docker-in-Docker) per environment,
  wired to the agent over a shared socket volume. Run `docker compose` inside
  the agent and it talks to its own daemon.
- A **shared Traefik proxy** that routes `<env>.pinchy.localhost:{8080,4096,3000}`
  to the right agent. Active healthchecks on each backend mean compose-based
  services (reachable via the dind container) and in-agent processes both work
  transparently.
- A **discovery console** at `http://console.pinchy.localhost:8080` — a live
  HTML dashboard that lists every pinchy environment, its opencode sessions, and
  provides deep links into each session's web console. The dashboard auto-refreshes
  every 15 seconds. No JavaScript framework; served from the same shared network
  as the proxy.
- Label-driven discovery — no database, no config files, no daemons beyond
  Docker itself.

## Architecture

```
  host
  ──────────────────────────────────────────────────────────────
  :8080  :4096  :3000              pinchy-proxy (Traefik)
   │                │
   │  Host(console.pinchy.localhost)
   │                │  Host(<env>.pinchy.localhost)
   ▼                ▼
  ╔════════════════════════════════╗
  ║          pinchy-shared         ║  (bridge network)
  ╚════════════════════════════════╝
     │              │          │
     ▼              ▼          ▼
  ┌──────────┐  ┌──────────┐  ┌──────────────┐
  │ console  │  │  agent   │  │  docker      │
  │          │  │          │  │  (dind-      │
  │ dashboard│  │ opencode │  │  rootless)   │
  │ :8080    │  │ web :4096│  │              │
  └──────────┘  └──────┬───┘  └──────┬───────┘
                       │             │
                       └──── sock ───┘
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
make images      # build agent, docker, proxy, and console images locally
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
5. Print the OpenCode web UI URL and open it in your default browser.

Pass `--no-browser` to skip the browser open and just print the URL.

### Access it

| Method | Address |
|--------|---------|
| Browser | `http://example.pinchy.localhost:4096/` |
| Open web UI | `pinchy open example` |
| Shell | `pinchy shell example` |
| Attach local opencode | `opencode attach http://example.pinchy.localhost:4096` |

### Attach with the opencode CLI

If you have `opencode` installed on the host, you can drive a pinchy
environment from your local terminal instead of the browser. Each agent's
opencode web server is reachable through the shared proxy at:

```
http://<env>.pinchy.localhost:4096/
```

Attach to it with:

```
opencode attach http://example.pinchy.localhost:4096
```

You get the local opencode TUI while the agent, its tools, and its dedicated
dind daemon all run inside the pinchy environment.

`pinchy open <name>` prints the same URL, so the env name is the only thing
you need to remember.

## Commands

| Command | Description |
|---------|-------------|
| `pinchy init` | Initialise shared proxy infrastructure (proxy + console) |
| `pinchy create <name>` | Create and start a new environment |
| `pinchy ls` | List environments, proxy, and console status |
| `pinchy open <name>` | Print and open the OpenCode web UI for an environment |
| `pinchy shell <name>` | Open an interactive shell in a service container |
| `pinchy exec <name> -- <cmd>` | Run a one-off command in a service container |
| `pinchy logs <name>` | Stream logs from every container in an environment |
| `pinchy start <name>` | Start a stopped environment |
| `pinchy stop <name>` | Stop a running environment |
| `pinchy restart <name>` | Stop and start an environment |
| `pinchy rm <name>` | Remove an environment and its volumes |
| `pinchy proxy logs` | Fetch logs from the shared proxy container |
| `pinchy console logs` | Fetch logs from the shared console container |
| `pinchy llmproxy logs` | Fetch logs from the shared LLM proxy container |
| `pinchy version` | Print the pinchy version |

Run `pinchy <command> --help` for flags and full descriptions.

## Git worktrees

When you run `pinchy create <name>` from inside a git repository (or with
`--workdir` pointing into one), pinchy automatically creates a dedicated git
worktree for the environment:

```
<repo>/.pinchy-worktrees/<name>/
```

on a new branch also named `<name>` (branched from the current `HEAD`). That
worktree directory is then bind-mounted into the agent at `/data`, giving each
environment its own isolated branch while sharing the repository's history and
object store.

### Requirements

- `git` must be on your `PATH`
- a local branch named `<name>` must not already exist in the repository

### Opt out

Pass `--no-worktree` to use a plain bind-mount instead:

```
pinchy create example --no-worktree
```

### Cleanup

`pinchy rm` removes the worktree directory and deletes the branch by default.
Pass `--keep-worktree` to preserve both (e.g. to review or push the branch):

```
pinchy rm example --keep-worktree
```

### Gitignore note

The `.pinchy-worktrees/` directory will appear as untracked in `git status`.
Add it to `.gitignore` or `.git/info/exclude` to suppress the noise:

```
echo '/.pinchy-worktrees/' >> .git/info/exclude
```

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
  GITHUB_TOKEN: ghp_...

# Per-environment overrides (merged on top of the global env block).
environments:
  example:
    env:
      SOME_VAR: override-value

# LLM proxy (optional). When set, pinchy starts a shared LiteLLM container
# (pinchy-llmproxy) that holds the real Anthropic key. Agents are pre-wired
# to use it — they never receive the real key.
llm_proxy:
  anthropic_api_key: sk-ant-...
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

Plus three global shared services:

| Container | Image | Role |
|-----------|-------|------|
| `pinchy-proxy` | `ghcr.io/nickschuch/pinchy-proxy` | Traefik reverse proxy |
| `pinchy-console` | `ghcr.io/nickschuch/pinchy-console` | Discovery dashboard at `http://console.pinchy.localhost:8080` |
| `pinchy-llmproxy` | `ghcr.io/nickschuch/pinchy-llmproxy` | LiteLLM Anthropic proxy (when `llm_proxy` is configured); admin UI at `http://llmproxy.pinchy.localhost:8080` |

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
internal/console/   discovery dashboard server, poller, snapshot, HTML template
internal/dockerx/   Docker SDK helpers (container, network, volume, proxy, console)
internal/env/       label constants, name helpers, environment model
internal/config/    config file loading and validation
internal/gitx/      git worktree helpers (host-side git operations)
internal/table/     table rendering helper
images/agent/       agent Dockerfile (opencode + dev tooling)
images/console/     console Dockerfile (pinchy binary, serves dashboard)
images/docker/      dind-rootless Dockerfile
images/llmproxy/    LiteLLM-based Anthropic proxy Dockerfile
images/proxy/       Traefik Dockerfile
examples/           sample workloads
```

### Tests

```
make test
```

### Image override

All image references can be overridden at runtime:

```
PINCHY_AGENT_IMAGE=myregistry/agent:dev pinchy create example
PINCHY_DOCKER_IMAGE=myregistry/docker:dev pinchy create example
PINCHY_PROXY_IMAGE=myregistry/proxy:dev pinchy create example
PINCHY_CONSOLE_IMAGE=myregistry/console:dev pinchy init
PINCHY_LLMPROXY_IMAGE=myregistry/llmproxy:dev pinchy init
```

### LLM proxy (Anthropic)

Pinchy can run a shared [LiteLLM](https://github.com/BerriAI/litellm)-based
proxy so the real `ANTHROPIC_API_KEY` never reaches any agent container.

**How it works**

```
agent (opencode)
  ANTHROPIC_API_KEY=sk-pinchy-llmproxy-shared   ← non-secret, baked into image
  baseURL=http://pinchy-llmproxy:4000/anthropic/v1
        │
        ▼
pinchy-llmproxy (LiteLLM, internal to pinchy-shared bridge)
        │  real sk-ant-… key injected at container start
        ▼
api.anthropic.com
```

The hardcoded shared token (`sk-pinchy-llmproxy-shared`) grants access only
to the local proxy on the `pinchy-shared` bridge — never directly to Anthropic.

**Setup**

Add to `~/.config/pinchy/config.yaml`:

```yaml
llm_proxy:
  anthropic_api_key: sk-ant-...
```

Then run:

```
pinchy init
```

The proxy starts automatically. All new environments will use it.

**Admin UI**

The LiteLLM admin UI (request logs, spend, config) is exposed at:

```
http://llmproxy.pinchy.localhost:8080
```

Log in with the master key `sk-pinchy-llmproxy-shared`. The UI is reachable
only via the host's `.pinchy.localhost` resolution — it is not published on any
external interface.

**Rotating the Anthropic key**

Update `llm_proxy.anthropic_api_key` in the config file and re-run
`pinchy init`. Pinchy detects the key change (via a label hash), removes the
old container, and starts a fresh one. Running agent containers need no restart
because they never held the real key.

**Bypassing the proxy per-project**

To use a different Anthropic key for a specific project, override both in the
agent at create time:

```
pinchy create myenv -e ANTHROPIC_API_KEY=sk-ant-direct
```

Then add to your project's `opencode.json`:

```json
{
  "provider": {
    "anthropic": {
      "options": {
        "baseURL": "https://api.anthropic.com/v1"
      }
    }
  }
}
```

**Reserved names**

`llmproxy` is a reserved environment name (alongside `proxy` and `console`)
and cannot be used as a `pinchy create` argument.

**Troubleshooting**

If Anthropic calls fail with DNS or connection errors, the `pinchy-llmproxy`
container is not running. Check `pinchy ls` and `pinchy llmproxy logs`.
