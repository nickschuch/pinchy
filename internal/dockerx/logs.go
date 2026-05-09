package dockerx

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// LogStream identifies one source in a multiplexed log feed.
type LogStream struct {
	// ContainerID is the Docker container ID to follow.
	ContainerID string
	// Prefix is prepended to every output line, followed by "| ".
	Prefix string
}

// MultiLogs concurrently streams logs from each LogStream, prefixing every
// line with "<prefix>| " and writing to out. It returns when all streams
// terminate or ctx is cancelled. follow=true keeps the streams open.
func MultiLogs(ctx context.Context, cli *client.Client, streams []LogStream, follow bool, out io.Writer) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	errs := make([]error, len(streams))

	for i, s := range streams {
		wg.Add(1)
		go func(i int, s LogStream) {
			defer wg.Done()
			rc, err := cli.ContainerLogs(ctx, s.ContainerID, container.LogsOptions{
				ShowStdout: true,
				ShowStderr: true,
				Follow:     follow,
				Timestamps: false,
			})
			if err != nil {
				errs[i] = fmt.Errorf("logs %s: %w", s.Prefix, err)
				return
			}
			defer rc.Close()
			pr, pw := io.Pipe()
			// stdcopy demultiplexes the Docker log multiplex format into
			// stdout/stderr; we merge both into the same pipe.
			go func() {
				_, _ = stdcopy.StdCopy(pw, pw, rc)
				_ = pw.Close()
			}()
			scanner := bufio.NewScanner(pr)
			scanner.Buffer(make([]byte, 64*1024), 1024*1024)
			for scanner.Scan() {
				mu.Lock()
				fmt.Fprintf(out, "%s| %s\n", s.Prefix, scanner.Text())
				mu.Unlock()
			}
		}(i, s)
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
