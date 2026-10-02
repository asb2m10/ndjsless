package ui

import (
	"io"
	"os"

	"github.com/aymanbagabas/go-osc52/v2"
)

// copyToClipboard writes s to the system clipboard using OSC52, the terminal
// escape sequence most modern terminals (and tmux/screen, in passthrough
// mode) understand directly. w is the same file the bubbletea renderer
// writes to -- usually /dev/tty, since this program's real stdout may be a
// redirected log and not the terminal at all. A nil w (as in tests that build
// a Model without a Config.Clipboard) is a silent no-op.
func copyToClipboard(w io.Writer, s string) {
	if w == nil {
		return
	}
	seq := osc52.New(s)
	switch {
	case os.Getenv("TMUX") != "":
		seq = seq.Tmux()
	case os.Getenv("TERM") == "screen":
		seq = seq.Screen()
	}
	seq.WriteTo(w)
}
