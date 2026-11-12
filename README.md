# Pinchy

A self-contained [T3 Code](https://github.com/pingdotgg/t3code) server with
Claude Code, the GitHub CLI and Docker, packaged as a Docker Compose stack.

Agents work on repositories in a local `workspace/` directory and can run
`docker` commands against a sandboxed Docker-in-Docker daemon, not your
host's daemon.

## What's inside

| Service  | Image                         | Purpose                                                    |
| -------- | ----------------------------- | ---------------------------------------------------------- |
| `t3code` | Built from `Dockerfile`       | T3 Code server on port 3773, Claude Code, `gh`, `git`, `mise`, Node |
| `docker` | `docker:dind`                 | Docker daemon used by `t3code` over TLS                    |

Persistent state lives in named volumes:

- `t3-data`: T3 Code data (`/data/t3`) and Claude Code's login and settings (`/data/claude`).
- `dind-data`: images and containers created inside the sandbox.
- `dind-certs-client`: TLS client certs shared between the two services.

## Requirements

- Docker with the Compose plugin (v2.24 or newer, for optional `env_file`).
- A Claude account to log in to Claude Code.
- Optional: a GitHub personal access token for `gh` and `git` over HTTPS.

The `docker` service runs **privileged**, which Docker-in-Docker needs.

## Getting started

1. **Clone and enter the repo.**

   ```sh
   git clone https://github.com/<org>/pinchy.git
   cd pinchy
   ```

2. **Add your projects.** Clone the repositories you want to work on into
   `workspace/`. They appear in the container at `/workspace`. Apart from
   its README, the directory is git-ignored.

3. **Configure GitHub access (optional).**

   ```sh
   cp github.env.example github.env
   chmod 600 github.env
   ```

   Then fill in your git identity and a `GH_TOKEN`. The token is used by
   `gh` and by `git` for HTTPS remotes. SSH-style GitHub remotes
   (`git@github.com:...`) are rewritten to HTTPS automatically.
   `github.env` is git-ignored. See [GitHub token](#github-token) for the
   permissions it needs.

4. **Match your host user (Linux).** The container user defaults to
   UID/GID 1000. If yours differ, export them so files in `workspace/` keep
   the right ownership:

   ```sh
   export USER_UID=$(id -u) USER_GID=$(id -g)
   ```

   Put these in a `.env` file next to `docker-compose.yml` to make them stick.

5. **Build and start.**

   ```sh
   docker compose up -d --build
   ```

6. **Log in to Claude Code** (once; the login is kept in the `t3-data` volume):

   ```sh
   docker compose exec t3code claude auth login
   ```

7. **Pair your browser with T3 Code.** T3 Code requires a one-time pairing
   token before a browser can connect. The server prints pairing details when
   it starts:

   ```sh
   docker compose logs t3code
   ```

   Pairing tokens expire after a few minutes. If yours has expired, or you
   want to pair another browser, create a new one:

   ```sh
   docker compose exec t3code t3 auth pairing create \
     --base-dir /data/t3 --base-url http://localhost:3773
   ```

   Open the `/pair#token=...` link it prints. Each token works once. After
   pairing, the browser stays signed in and you can use
   <http://localhost:3773> directly.

## GitHub token

Agents use the token to clone, push branches, open and update pull
requests, and read CI results. Create it at
<https://github.com/settings/tokens>.

**Fine-grained token (recommended).** Set the resource owner to the user
or organisation that owns your repositories, pick the repositories, and
grant these repository permissions:

| Permission      | Access         | Why                                                     |
| --------------- | -------------- | ------------------------------------------------------- |
| Contents        | Read and write | Clone, fetch and push branches                          |
| Pull requests   | Read and write | Open, update, review and comment on PRs                 |
| Metadata        | Read-only      | Required by GitHub for all fine-grained tokens           |
| Commit statuses | Read-only      | Read CI status on PRs                                   |
| Actions         | Read-only      | Read workflow runs and logs                             |
| Issues          | Read and write | Optional: read and comment on issues                    |
| Workflows       | Read and write | Optional: only if agents edit `.github/workflows/` files |

A fine-grained token covers a single owner, so repositories across several
organisations need a classic token or one token each. Some organisations
must approve fine-grained tokens before they work.

**Classic token.** Select `repo` and `read:org`. Add `workflow` if agents
edit `.github/workflows/` files. If your organisation uses SAML single
sign-on, authorise the token for it under **Configure SSO**.

Check the token from inside the container:

```sh
docker compose exec t3code gh auth status
```

## Pairing tokens

Every new browser or device needs its own pairing token. Manage tokens from
the host:

```sh
docker compose exec t3code t3 auth pairing create --base-dir /data/t3 --ttl 15m --label laptop
docker compose exec t3code t3 auth pairing list --base-dir /data/t3
docker compose exec t3code t3 auth pairing revoke --base-dir /data/t3 <id>
```

Pass `--base-dir /data/t3` every time. Without it, `t3` looks in the wrong
data directory and the running server won't recognise the token.

## Configuration

Build arguments in `Dockerfile`:

| Argument              | Default  | Notes                                                    |
| --------------------- | -------- | -------------------------------------------------------- |
| `T3_VERSION`          | `0.1.8`  | T3 Code npm package version                              |
| `CLAUDE_CODE_VERSION` | `stable` | `stable`, `latest` or an exact version                   |
| `NODE_VERSION`        | `24`     | Node.js version installed with `mise`                    |
| `DOCKER_CLI_TAG`      | `cli`    | Pin it (e.g. `29-cli`) to match the `docker:dind` tag    |
| `USER_UID` / `USER_GID` | `1000` | Set through the environment, see step 4                 |

Extra language runtimes can be installed inside the container with `mise`
(for example `mise use -g go@latest`). They land in the container's home
directory, so rebuilds discard them. Add them to the `Dockerfile` to keep them.

## Common tasks

```sh
docker compose logs -f t3code          # follow server logs
docker compose exec t3code bash        # shell in the T3 Code container
docker compose build --pull --no-cache # upgrade T3 Code / Claude Code
docker compose down                    # stop (volumes are kept)
docker compose down -v                 # stop and delete all state, including the Claude login
```

## Security notes

- Port 3773 is published on all host interfaces. To keep it local, change
  the mapping to `"127.0.0.1:3773:3773"`.
- Agents in this container can run anything the Docker daemon allows,
  inside the privileged `docker` service. Treat the stack as you would
  a remote shell on your machine.
- `GH_TOKEN` is available to every process in the container. Give it the
  narrowest permissions you need and limit it to the repositories you work
  on (see [GitHub token](#github-token)).

## Roadmap

- **Per-thread sandboxing:** isolate each T3 Code thread in its own
  sandbox, not one shared container and Docker daemon.
- **Central MCP gateway support:** connect agents to MCP servers through
  a shared gateway, not per-container configuration.

## Troubleshooting

- **`tls: failed to verify certificate ... not dind`**: the daemon service
  must be named `docker`; its generated cert is only valid for that name.
- **Permission denied writing to `/workspace`**: `workspace/` is owned by
  a different user than `USER_UID`/`USER_GID` (for example, if it was
  deleted and Docker recreated it as root). Run
  `sudo chown -R $(id -u):$(id -g) workspace`.
