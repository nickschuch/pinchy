package cli

import (
	"context"
	"time"
)

// waitContext returns a derived context with a timeout cap. If timeout is
// non-positive, it returns the parent context unchanged together with a
// no-op cancel.
func waitContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, timeout)
}
