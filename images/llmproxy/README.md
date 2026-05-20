# pinchy-llmproxy

A [LiteLLM](https://github.com/BerriAI/litellm)-based Anthropic API proxy that
keeps the real `ANTHROPIC_API_KEY` off every agent container.

## How it fits in

```
agent (opencode)
  ANTHROPIC_API_KEY=sk-pinchy-llmproxy-shared   ← non-secret shared token
  baseURL=http://pinchy-llmproxy:4000/anthropic/v1
        │
        ▼  x-api-key: sk-pinchy-llmproxy-shared
pinchy-llmproxy (this image)
        │
        ▼  x-api-key: <real sk-ant-…>
api.anthropic.com
```

The real `ANTHROPIC_API_KEY` is injected at container start time from
`~/.config/pinchy/config.yaml` (`llm_proxy.anthropic_api_key`). It is never
written into this image.

## Admin UI

LiteLLM's built-in admin UI is exposed through the shared Traefik proxy at:

```
http://llmproxy.pinchy.localhost:8080
```

Log in with the master key `sk-pinchy-llmproxy-shared`.

## Pinned LiteLLM version

The upstream image tag is pinned in the `Dockerfile`. To upgrade:

1. Update the `FROM` line to the desired release tag.
2. Run `make images` to rebuild.
3. Run `pinchy init --recreate` to replace the running container.

## Rebuilding locally

```
make images
```

or just this image:

```
docker build -t ghcr.io/nickschuch/pinchy-llmproxy:latest images/llmproxy
```
