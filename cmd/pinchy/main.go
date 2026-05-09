// Pinchy is a CLI for managing containerised "agent" development
// environments backed by a dedicated rootless docker-in-docker daemon.
//
// Each environment is a labelled pair of containers (an opencode-driven
// agent and its private dind daemon) sharing a per-environment socket
// volume. Environments are discovered via Docker labels, so pinchy holds
// no on-disk state of its own.
package main

import (
	"context"
	"os"

	"github.com/charmbracelet/fang"

	"github.com/nickschuch/pinchy/internal/cli"
)

// version is overridden at build time via -ldflags '-X main.version=...'.
var version = "dev"

func main() {
	root := cli.NewRoot(version)
	err := fang.Execute(
		context.Background(),
		root,
	)
	if err != nil {
		os.Exit(1)
	}
}
