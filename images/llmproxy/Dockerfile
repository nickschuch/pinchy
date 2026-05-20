# pinchy-llmproxy: a LiteLLM-based Anthropic API proxy.
#
# The real ANTHROPIC_API_KEY is never baked into this image. It is supplied at
# container start time via the ANTHROPIC_API_KEY environment variable, which
# pinchy reads from the host config file (llm_proxy.anthropic_api_key) and
# injects when it creates the container.
#
# Agent containers authenticate with the hardcoded master key
# sk-pinchy-llmproxy-shared (see images/agent/Dockerfile and
# internal/env/llmproxy.go). That token grants access only to this local
# proxy on the pinchy-shared bridge network, never directly to Anthropic.
#
# The admin UI is accessible at http://llmproxy.pinchy.localhost:8080 via the
# shared Traefik proxy. Log in with the master key above.
FROM docker.litellm.ai/berriai/litellm:v1.85.0

COPY config.yaml /app/pinchy-config.yaml

EXPOSE 4000

ENTRYPOINT ["litellm", "--config", "/app/pinchy-config.yaml", "--port", "4000", "--host", "0.0.0.0"]
