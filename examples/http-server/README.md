# http-server example

A minimal Go HTTP server for verifying pinchy agent-environment routing.
Run it inside a pinchy agent environment and curl the endpoints to confirm
traffic reaches the service on port 8080.

## Quick start

Inside a pinchy agent environment:

```bash
docker compose up --build
```

## Endpoints

| Endpoint       | Response                                              |
|----------------|-------------------------------------------------------|
| `GET /`        | Hostname, method, path, remote, and forwarded headers |
| `GET /healthz` | `ok`                                                  |

## Verify via the pinchy proxy

Replace `<env>` with your environment name:

```bash
curl http://<env>.pinchy.localhost:8080/
curl http://<env>.pinchy.localhost:8080/healthz
```

Traffic is routed by pinchy's Traefik proxy using dual-backend failover:
the dind container (serving the compose service) and the agent container
are both registered as backends. Active healthchecks on `/healthz` ensure
requests are only forwarded to a healthy backend.

## Notes

- See `AGENTS.md` for agent-specific context, conventions, and editing tips.
- The server binds `0.0.0.0:8080`; the pinchy agent environment routes traffic
  to that port automatically via `<env>.pinchy.localhost:8080`.
- Std-lib only — no external Go dependencies.
- Existing environments created before dual-backend routing was added need
  to be recreated (`pinchy rm <env> && pinchy create <env>`) to get failover.
