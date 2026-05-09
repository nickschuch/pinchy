FROM docker:29-dind-rootless

USER root

# Pre-create /run/user/1000 owned by the rootless user so that when a named
# volume is mounted here, Docker initialises it with the right ownership.
# Without this, the volume is created owned by root and dockerd-entrypoint.sh
# fails with "need writable XDG_RUNTIME_DIR for user 1000".
RUN mkdir -p /run/user/1000 && \
    chown rootless:rootless /run/user/1000 && \
    chmod 0700 /run/user/1000

USER rootless
