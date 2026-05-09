BIN      := bin/pinchy
PKG      := ./cmd/pinchy
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -X main.version=$(VERSION)

REGISTRY     ?= ghcr.io/nickschuch
AGENT_IMAGE  ?= $(REGISTRY)/pinchy-agent:latest
DOCKER_IMAGE ?= $(REGISTRY)/pinchy-docker:latest
PROXY_IMAGE  ?= $(REGISTRY)/pinchy-proxy:latest

.PHONY: help build test images clean

help:
	@echo "Usage:"
	@echo "  make build    Build the pinchy binary into ./bin/pinchy"
	@echo "  make test     Run unit tests"
	@echo "  make images   Build the agent, docker, and proxy images locally"
	@echo "  make clean    Remove build artefacts"

build:
	go build -ldflags '$(LDFLAGS)' -o $(BIN) $(PKG)

test:
	go test ./...

images:
	docker build -t $(AGENT_IMAGE) images/agent
	docker build -t $(DOCKER_IMAGE) images/docker
	docker build -t $(PROXY_IMAGE) images/proxy

clean:
	rm -rf bin/
