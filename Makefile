BIN      := bin/pinchy
PKG      := ./cmd/pinchy
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -X main.version=$(VERSION)

REGISTRY        ?= ghcr.io/nickschuch
AGENT_IMAGE     ?= $(REGISTRY)/pinchy-agent:latest
DOCKER_IMAGE    ?= $(REGISTRY)/pinchy-docker:latest
PROXY_IMAGE     ?= $(REGISTRY)/pinchy-proxy:latest
CONSOLE_IMAGE   ?= $(REGISTRY)/pinchy-console:latest
LLMPROXY_IMAGE  ?= $(REGISTRY)/pinchy-llmproxy:latest

.PHONY: help build test images images-console images-llmproxy clean

help:
	@echo "Usage:"
	@echo "  make build             Build the pinchy binary into ./bin/pinchy"
	@echo "  make test              Run unit tests"
	@echo "  make images            Build all images (agent, docker, proxy, console, llmproxy)"
	@echo "  make images-console    Build only the console image locally"
	@echo "  make images-llmproxy   Build only the llmproxy image locally"
	@echo "  make clean             Remove build artefacts"

build:
	go build -ldflags '$(LDFLAGS)' -o $(BIN) $(PKG)

test:
	go test ./...

images: images-console images-llmproxy
	docker build --no-cache -t $(AGENT_IMAGE) images/agent
	docker build --no-cache -t $(DOCKER_IMAGE) images/docker
	docker build --no-cache -t $(PROXY_IMAGE) images/proxy

images-console:
	docker build --no-cache -t $(CONSOLE_IMAGE) -f images/console/Dockerfile .

images-llmproxy:
	docker build --no-cache -t $(LLMPROXY_IMAGE) images/llmproxy

clean:
	rm -rf bin/
