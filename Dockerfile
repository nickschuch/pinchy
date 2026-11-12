# syntax=docker/dockerfile:1

# Pin this (e.g. 29-cli) to match the dind tag in docker-compose.yml.
ARG DOCKER_CLI_TAG=cli

# Static docker CLI + buildx/compose plugins from the official image.
FROM docker:${DOCKER_CLI_TAG} AS docker-cli

# T3 Code's Linux binary needs glibc, so this side stays on Debian.
# The daemon runs in the separate docker:dind service.
FROM debian:trixie-slim

ARG USER_UID=1000
ARG USER_GID=1000
ARG USERNAME=t3
ARG T3_VERSION=0.1.8
ARG NODE_VERSION=24
# "stable", "latest", or an exact version like 2.1.89
ARG CLAUDE_CODE_VERSION=stable

RUN apt-get update && apt-get install -y --no-install-recommends \
        bash ca-certificates curl gh git gnupg openssh-client procps xz-utils \
    && rm -rf /var/lib/apt/lists/*

# Git authenticates to GitHub over HTTPS with GH_TOKEN. SSH-style remotes are
# rewritten to HTTPS so they use the same token.
RUN git config --system credential.https://github.com.helper '!gh auth git-credential' \
    && git config --system url."https://github.com/".insteadOf git@github.com: \
    && git config --system --add url."https://github.com/".insteadOf ssh://git@github.com/

COPY --from=docker-cli /usr/local/bin/docker /usr/local/bin/docker
COPY --from=docker-cli /usr/local/libexec/docker/cli-plugins/ /usr/local/libexec/docker/cli-plugins/

# mise binary is system-wide; everything it installs lives in the user's home.
RUN curl -fsSL https://mise.run | MISE_INSTALL_PATH=/usr/local/bin/mise sh \
    && mise --version

# Non-root user matching the host UID/GID. /data and /workspace are owned
# by it so a fresh named volume mounted at /data inherits that ownership.
RUN groupadd --gid ${USER_GID} ${USERNAME} \
    && useradd --uid ${USER_UID} --gid ${USER_GID} --create-home --shell /bin/bash ${USERNAME} \
    && mkdir -p /data /workspace \
    && chown ${USER_UID}:${USER_GID} /data /workspace

USER ${USER_UID}:${USER_GID}
ENV HOME=/home/${USERNAME} \
    MISE_YES=1 \
    CLAUDE_CONFIG_DIR=/data/claude
ENV PATH="${HOME}/.local/bin:${HOME}/.local/share/mise/shims:${PATH}"

RUN echo 'eval "$(mise activate bash)"' >> ~/.bashrc

# Node + T3 Code via mise. `t3 --version` makes the launcher download
# the T3 binary into ~/.t3 now, not on first start.
RUN mise use -g node@${NODE_VERSION} npm:t3@${T3_VERSION} \
    && mise reshim \
    && t3 --version

# Claude Code, native installer (installs to ~/.local/bin/claude).
# CLAUDE_CONFIG_DIR keeps login + settings on the t3-data volume.
RUN curl -fsSL https://claude.ai/install.sh | bash -s ${CLAUDE_CODE_VERSION} \
    && claude --version

WORKDIR /workspace
EXPOSE 3773

ENTRYPOINT ["t3", "serve"]
CMD ["--host", "0.0.0.0", "--port", "3773", "--base-dir", "/data/t3", "/workspace"]
