# http-server example

## Purpose

A minimal Go HTTP server used to verify that pinchy agent-environment routing
correctly reaches a service running on port 8080. Spin it up with Docker
Compose inside a pinchy agent environment and curl the endpoints to confirm
that traffic flows through.

## Layout

| File                  | Role                                                     |
|-----------------------|----------------------------------------------------------|
| `main.go`             | Go HTTP server — two handlers (`/` and `/healthz`)       |
| `go.mod`              | Standalone Go module; not part of the root pinchy module |
| `Dockerfile`          | Multi-stage build; runtime image is `alpine:3.20`        |
| `docker-compose.yaml` | Builds and runs the server, published on `0.0.0.0:8080`  |
| `README.md`           | Human-facing quick-start                                 |

## How to run

Inside a pinchy agent environment, start the server with:

```bash
docker compose up --build
```

Verify via the pinchy proxy (replace `<env>` with your environment name):

```bash
curl http://<env>.pinchy.localhost:8080/        # returns hostname, method, path, headers
curl http://<env>.pinchy.localhost:8080/healthz # returns: ok
```

Or directly if you need to bypass the proxy:

```bash
curl http://localhost:8080/
```

## How routing works

Pinchy's Traefik proxy routes `Host(<env>.pinchy.localhost)` traffic to two
backends for each port (8080, 4096, 3000):

1. **Agent container** — serves traffic when an application is running directly
   in the agent shell (e.g. `go run .`).
2. **Dind container** — serves traffic when a service is running via
   `docker compose` inside the inner Docker daemon. Port-published services
   are reachable at the dind container's IP on `pinchy-shared` thanks to
   rootlesskit's port driver.

Active healthchecks on `/healthz` (every 5 s) mark an unhealthy backend as
unavailable, so Traefik automatically concentrates traffic on whichever
backend is actually serving — no manual reconfiguration required.

**Note:** for a few seconds after `docker compose up`, both backends are
being probed. During this window roughly half of requests may return 502 as
Traefik round-robins to the agent (which has no listener). This resolves
once the first healthcheck cycle completes (~5 s).

## Conventions and constraints

- **Std-lib only.** Do not add third-party dependencies.
- **Independent Go module** (`example.com/http-server`). It is deliberately
  separate from `github.com/nickschuch/pinchy`; do not add it to a Go workspace or
  reference it from the root `go.mod`.
- **Bind address is `0.0.0.0:8080`.** The pinchy agent environment already
  routes traffic to that port; do not change it without updating all files
  listed in the editing tip below.
- **Multi-stage Dockerfile.** Keep the runtime layer small; `alpine:3.20` is
  used so `wget`-based healthchecks and `docker exec sh` remain available.

## Not in scope

- Traefik labels (the agent environment handles routing).
- TLS or persistent storage.
- Authentication.

## Editing tips for agents

When changing the port, update **all four** of these in the same commit:

1. `main.go` — default value of `PORT`
2. `Dockerfile` — `EXPOSE` instruction
3. `docker-compose.yaml` — `ports` mapping and `environment.PORT`
4. `README.md` — curl examples

When adding a new route, add the handler in `main.go` and document it in
`README.md`.
