// Package source turns a reader into a stream of log lines, optionally following
// it the way tail -f does.
package source

import (
	"bufio"
	"context"
	"io"
	"time"
)

// maxLine is the largest single record we accept. Structured logs with embedded
// payloads blow past bufio's 64 KiB default routinely.
const maxLine = 4 << 20

// pollInterval is how long we wait before re-reading a file that hit EOF.
const pollInterval = 100 * time.Millisecond

// Lines streams lines from r. The channel closes when the input is exhausted;
// in follow mode a file is re-read after EOF so appends show up, and a pipe
// simply blocks until the writer closes it.
func Lines(ctx context.Context, r io.Reader, follow bool) <-chan string {
	out := make(chan string, 1024)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), maxLine)
		for {
			for sc.Scan() {
				select {
				case out <- sc.Text():
				case <-ctx.Done():
					return
				}
			}
			// Scan stopped: either a real error, or EOF.
			if sc.Err() != nil || !follow {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(pollInterval):
			}
			// A Scanner will not resume after EOF, so start a fresh one on the
			// same reader; the file offset is already past what we consumed.
			sc = bufio.NewScanner(r)
			sc.Buffer(make([]byte, 0, 64*1024), maxLine)
		}
	}()
	return out
}
