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
            libgcc

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

RUN curl -fsSL https://opencode.ai/install | bash
ADD opencode.json /home/skpr/.opencode/opencode.json
ADD environment.md /home/skpr/.opencode/environment.md

ENV PATH=$PATH:/home/skpr/.local/bin:/home/skpr/.opencode/bin

WORKDIR /data

# Run the opencode web server as PID 1. The web UI is reachable at
# http://<env>.pinchy.localhost:4096/ via the pinchy proxy. Attach a TUI with:
#   pinchy session <env>
# or directly with:
#   docker exec -it pinchy-<env>-agent opencode attach http://localhost:4096
ENTRYPOINT ["opencode", "web", "--hostname", "0.0.0.0", "--port", "4096"]
