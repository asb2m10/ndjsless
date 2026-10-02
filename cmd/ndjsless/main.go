// Command ndjsless pages ndjson logs the way less pages text.
//
//	tail -f app.log | ndjsless
//	ndjsless -f app.log
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/asb2m10/ndjsless/internal/record"
	"github.com/asb2m10/ndjsless/internal/source"
	"github.com/asb2m10/ndjsless/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ndjsless:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		fields  = flag.String("fields", "", "comma-separated fields to show, replacing the defaults")
		add     = flag.String("add", "", "comma-separated fields to add to the default columns")
		tsField = flag.String("ts-field", record.DefaultTsField, "field holding the event timestamp")
		follow  = flag.Bool("follow", false, "keep reading as the file grows, like tail -f")
		noColor = flag.Bool("no-color", false, "disable colour output")
	)
	flag.BoolVar(follow, "f", false, "shorthand for -follow")
	flag.Usage = usage
	flag.Parse()

	cols, err := columns(*fields, *add, *tsField)
	if err != nil {
		return err
	}

	// Open the input before the terminal, so a bad flag or an unreadable file
	// reports itself rather than being masked by the tty check below.
	reader, title, followSrc, closeAll, err := openInput(flag.Args(), *follow)
	if err != nil {
		return err
	}
	defer closeAll()

	// Keys must come from the terminal: in the common case stdin carries the log,
	// not the keyboard.
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("not a terminal; ndjsless is an interactive pager (%w)", err)
	}
	defer tty.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := ui.Config{
		Columns: cols,
		TsField: *tsField,
		Title:   title,
		Follow:  followSrc,
		Color:   !*noColor && ui.ColourEnabled(),
	}

	p := tea.NewProgram(
		ui.New(cfg, source.Lines(ctx, reader, followSrc)),
		tea.WithInput(tty),
		tea.WithOutput(tty),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err = p.Run()
	return err
}

// columns resolves the column list. --fields replaces the defaults; --add
// appends to them, keeping message last so the widest field stays at the edge.
func columns(fields, add, tsField string) ([]string, error) {
	if fields != "" {
		cols := split(fields)
		if len(cols) == 0 {
			return nil, fmt.Errorf("-fields: no field names given")
		}
		return cols, nil
	}
	cols := []string{tsField}
	cols = append(cols, split(add)...)
	return append(cols, record.MessageField), nil
}

func split(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// openInput returns the reader to page, a title for the status bar, whether the
// source should be followed, and a cleanup func.
func openInput(args []string, follow bool) (io.Reader, string, bool, func(), error) {
	noop := func() {}
	if len(args) == 0 {
		// A pipe blocks on read until the writer closes, so following is implicit;
		// starting pinned to the bottom is what you want from a live tail.
		return os.Stdin, "stdin", true, noop, nil
	}
	if follow && len(args) > 1 {
		return nil, "", false, noop, fmt.Errorf("-follow needs a single file, got %d", len(args))
	}

	files := make([]*os.File, 0, len(args))
	closeAll := func() {
		for _, f := range files {
			f.Close()
		}
	}
	readers := make([]io.Reader, 0, len(args))
	for _, name := range args {
		f, err := os.Open(name)
		if err != nil {
			closeAll()
			return nil, "", false, noop, err
		}
		files = append(files, f)
		readers = append(readers, f)
	}

	title := args[0]
	if len(args) > 1 {
		title = fmt.Sprintf("%s +%d more", args[0], len(args)-1)
	}
	if len(readers) == 1 {
		return readers[0], title, follow, closeAll, nil
	}
	return io.MultiReader(readers...), title, false, closeAll, nil
}

func usage() {
	fmt.Fprint(flag.CommandLine.Output(), `ndjsless — a less-like pager for ndjson logs

Usage:
  ndjsless [flags] [file ...]
  tail -f app.log | ndjsless

By default only the timestamp and message fields are shown. Press enter on a line
to see the whole record, with any JSON embedded in it indented. Lines that are not
valid JSON are shown verbatim as the message.

Flags:
`)
	flag.PrintDefaults()
	fmt.Fprint(flag.CommandLine.Output(), `
Keys:
  j/k d/u f/b g/G   scroll            h/l 0/$   scroll sideways
  /  n  N           search            enter     full record
  F                 toggle follow     ?         help          q  quit
`)
}
