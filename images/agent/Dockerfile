FROM alpine:3.23

RUN apk add git \
            bash \
            curl \
            ripgrep \
            coreutils \
            make \
            docker-cli \
            docker-cli-compose \
            ncurses-terminfo-base \
            musl-locales \
            libstdc++ \
            libgcc \
            gcompat 

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
ENV TERM=xterm-256color

ENV MISE_DATA_DIR="/mise"
ENV MISE_CONFIG_DIR="/mise"
ENV MISE_CACHE_DIR="/mise/cache"
ENV MISE_INSTALL_PATH="/usr/local/bin/mise"
ENV PATH="/mise/shims:$PATH"

RUN curl https://mise.run | sh

# Say yes to all the prompts (e.g. trust)
ENV MISE_YES=1

# Consistent UID between container and workstation.
RUN adduser -D -u 1000 skpr

# Pre-create skpr-owned dirs.
RUN mkdir -p /home/skpr/.local/bin && \
    chown -R skpr:skpr /home/skpr/.local

USER skpr

# Pre-wire the agent to authenticate against the shared pinchy-llmproxy
# container. This token is intentionally non-secret — it only authorises calls
# to the local LLM proxy on the pinchy-shared bridge network, never directly
# to Anthropic. The real ANTHROPIC_API_KEY lives only inside pinchy-llmproxy,
# sourced from the host config at container start time.
# MUST match: internal/env/llmproxy.go LLMProxyToken
#             images/llmproxy/config.yaml general_settings.master_key
ENV ANTHROPIC_API_KEY=sk-pinchy-llmproxy-shared

RUN curl -fsSL https://opencode.ai/install | bash
ADD opencode.json /home/skpr/.opencode/opencode.json
ADD environment.md /home/skpr/.opencode/environment.md

ENV PATH=$PATH:/home/skpr/.local/bin:/home/skpr/.opencode/bin

WORKDIR /data

# Health endpoint is /global/health on port 4096 (must match the path used in
# internal/env/labels.go ProxyTraefikLabelsAgent so Traefik and pinchy agree).
# --retries=60 × --interval=2s ≈ 2 min, matching --health-timeout default.
HEALTHCHECK --interval=2s --timeout=2s --start-period=5s --retries=60 \
    CMD curl -fsS http://localhost:4096/global/health || exit 1

# Run the opencode web server as PID 1. The web UI is reachable at
# http://<env>.pinchy.localhost:4096/ via the pinchy proxy:
#   pinchy open <env>
ENTRYPOINT ["opencode", "web", "--hostname", "0.0.0.0", "--port", "4096"]
