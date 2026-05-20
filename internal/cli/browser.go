package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
)

// envWebURL returns the OpenCode web UI URL for the named environment.
func envWebURL(name string) string {
	return "http://" + name + ".pinchy.localhost:4096/"
}

// printWebURL writes a prominent single-line URL notice to w.
func printWebURL(w io.Writer, name string) {
	fmt.Fprintf(w, "OpenCode web UI: %s\n", envWebURL(name))
}

// openInBrowser launches url in the user's default browser in the background.
// The function returns without waiting for the browser process; failures (e.g.
// no browser configured on a headless host) are treated as non-fatal: the
// caller is expected to print the URL separately so the user can still access
// the UI manually.
//
// A context is accepted for future extension; the browser launch itself is not
// context-aware because the spawned process is intentionally detached.
func openInBrowser(_ context.Context, url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		// Linux / WSL — prefer xdg-open, fall back to wslview.
		if _, err := exec.LookPath("xdg-open"); err == nil {
			cmd = exec.Command("xdg-open", url)
		} else if _, err := exec.LookPath("wslview"); err == nil {
			cmd = exec.Command("wslview", url)
		} else {
			return fmt.Errorf("no browser opener found (tried xdg-open, wslview); open %s manually", url)
		}
	}
	// Run detached — do not wait, do not capture output.
	return cmd.Start()
}
