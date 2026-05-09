# Environment

You are operating inside a containerized Linux development environment.

## System

- Alpine Linux 3.23, musl libc (not glibc — some prebuilt binaries need
  `libstdc++` / `libgcc` apk packages to run)
- Non-root user `skpr` (uid 1000), home at `/home/skpr`
- Working directory `/data` is a bind-mounted project from the host;
  treat it as the user's codebase
- A project-local `AGENTS.md` at `/data/AGENTS.md` may extend or override
  these instructions — read it if present

## Installed CLI tools

- `git` — version control
- `bash`, `coreutils` — shell and standard utilities
- `curl` — HTTP client
- `make` — build automation
- `ripgrep` (`rg`) — fast recursive search; prefer over `grep -r` and `find`
- `mise` — runtime/toolchain version manager; respects `.mise.toml` and
  `.tool-versions`. `MISE_YES=1` so prompts auto-accept
- `docker`, `docker buildx`, `docker compose` — rootless Docker; the daemon
  runs in the background and its log tails in the Docker Zellij tab at
  `/tmp/dockerd.log`. No `sudo`, no `--privileged` semantics; some kernel
  features (raw networking, ports <1024) are unavailable
- `fuse-overlayfs`, `slirp4netns` — used internally by rootless Docker, not
  invoked directly

## Conventions

- Use `rg` for code search
- The Docker daemon is rootless: bind mounts go through user-namespace
  uid/gid mapping, and `docker run --privileged` from inside this
  environment does not grant true host privilege
- Do not assume systemd; background processes are launched directly
