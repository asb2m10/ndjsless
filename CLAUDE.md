# CLAUDE.md

`ndjsless` is a `less`-style terminal pager for ndjson application logs. See `README.md` for the
user-facing feature set and key bindings.

## Commands

```sh
go build ./cmd/ndjsless          # binary in ./ndjsless (gitignored)
go vet ./... && gofmt -l .       # gofmt -l must print nothing
go test ./...
go test ./internal/record -run TestPrettyIndentsJSONEmbeddedInAMessage -v
go test ./internal/ui -run XXX -bench . -benchtime 50x   # render-cost guards
```

Running it needs a terminal, so it cannot be driven from a plain tool call:

```sh
./ndjsless testdata/sample.ndjson        # fixture covers broken lines, embedded JSON, odd timestamps
tail -f app.log | ndjsless
```

## Architecture

Four packages, one direction of dependency: `record` ← `ui` → (`source` feeds it a channel), wired by
`cmd/ndjsless`.

- **`internal/record`** — `Parse(line, tsField) Record`. Pure, no I/O, no terminal concepts. A
  `Record` keeps `Raw` always; `Fields` is nil and `Broken` is true when the line is not a JSON
  object. `embed.go` holds the popup pretty-printer.
- **`internal/source`** — `Lines(ctx, r, follow) <-chan string`. A `bufio.Scanner` with a 4 MiB
  buffer; in follow mode it builds a *fresh* Scanner after EOF, because a Scanner will not resume.
- **`internal/ui`** — the bubbletea model. `model.go` is state and `Update`, `view.go` is row
  layout, `popup.go` the full-screen, borderless popup, `style.go` the lipgloss styles.
- **`cmd/ndjsless`** — flags, input selection, terminal wiring.

### Invariants that are easy to break

**Keys come from `/dev/tty`, never stdin.** In the primary use case stdin carries the log. `main.go`
opens `/dev/tty` and passes it as both `tea.WithInput` and `tea.WithOutput`. Anything that reads
`os.Stdin` for interaction breaks `tail -f app.log | ndjsless`. The input is opened *before* the tty
so a bad flag or unreadable file reports its real error instead of "not a terminal".

**Rendering never touches more of a record than can appear on screen.** A single record can carry a
megabyte. `Model.rowText` clips each column to `xoff + w + 8` cells *before* any O(n) work
(`clipRunes`, then `sanitise`), and `maxXOff` reads `Model.tailW` — the last column's display width,
measured once per record at ingest — rather than re-rendering rows. Building or measuring whole
records in the render path cost 127 ms/frame before this; `BenchmarkViewHugeLines` must stay level
with `BenchmarkViewNormalLines`. `Model.hasJSON` is cached at ingest for the same reason.
`TestClippingDoesNotChangeWhatIsDisplayed` pins the clipped output against a slow reference.

**Style is applied after the horizontal slice, never before.** `ansi.Cut`/`StringWidth` count display
cells; escape sequences in the input to that arithmetic corrupt it. Build the row plain, slice, then
`highlight()`.

**No word wrap in the list — one record is one row.** `sanitise()` flattens embedded newlines, tabs
and carriage returns. Wrapping exists only in `popup.go` (`wrappedPopup`).

**Column widths only ever grow**, clamped to `colClamp`; shrinking makes the view jitter sideways as
logs stream. The timestamp column is fixed at `tsWidth`; the last column is unbounded (`width == 0`)
and unpadded. Header labels are clipped to their column or the header drifts out of alignment.

**A multi-rune `KeyRunes` message is one key per rune.** Bubbletea coalesces a held or fast-repeated
key into a single message, so `msg.String()` is `"jjjjj"` and matches no case. `handleKey` replays
bursts rune by rune; in `modeSearch` a burst is appended whole, since that is a paste.

**`Model` is a value.** `Update` and the key handlers take it by value and must return the mutated
copy; pointer-receiver helpers (`append`, `moveCursor`, `setSearch`) mutate that local copy, which is
why they work.

**Filtering runs off the UI goroutine.** `applyFilter` blanks `rows` and returns a `tea.Cmd` that
scans a snapshot of `recs`; `finishFilter` installs the result (dropped if `filterGen` moved on) and
admits records that arrived meanwhile. This is only safe because `recs` is append-only and a parsed
`Record` is never mutated — keep it that way.

### Embedded JSON: two shapes, deliberately rendered differently

`record.Pretty` is the popup body.

- A string that is *nothing but* JSON (`"payload":"{...}"`) is expanded in place by `expand()`, so it
  indents as real structure.
- A string that merely *contains* JSON (`"message":"request failed: {...}"` — the common logger
  shape) keeps its true value, and `sections()` appends an indented
  `--- embedded in <dotted.path> ---` block. `findJSON` locates a balanced fragment while ignoring
  brackets inside string literals.
- A `Broken` line is shown verbatim, then the same rule via `prettyBroken`.

Map keys are sorted (`json.Marshal` on a map, plus `sort.Strings` in `walkStrings`) so the popup is
byte-identical between renders.

**Known limitation:** `maxEmbedDepth = 4` bounds the *total* walk depth, not the number of embedded
unwraps. JSON inside `{"a":{"b":{"c":{"d":"{...}"}}}}` is therefore left escaped, even though it is
only one level of embedding. Counting embeds separately from record nesting would fix it.

### Dependency pin

`github.com/charmbracelet/x/ansi` must stay at the version the module graph picks for lipgloss 1.1.0
and bubbletea 1.3.10 (currently v0.10.1). `go get`ting `x/ansi` on its own pulls a newer release that
fails to compile against the `x/cellbuf` those two pin.

### No mouse-driven scrolling (by design, for now)

`cmd/ndjsless/main.go` deliberately does not pass `tea.WithMouseCellMotion()` (or any mouse mode) to
`tea.NewProgram`. Enabling mouse reporting makes the terminal forward clicks and drags to the app
instead of letting the terminal's own text selection handle them — on every terminal, enabling mouse
mode and keeping native mouse text selection are mutually exclusive. `internal/ui/model.go`'s
`handleMouse` (wheel scroll/pan) is therefore dead code today: no `tea.MouseMsg` is ever produced
without a mouse mode enabled, but it's left in place rather than deleted in case this gets revisited.

Fix landed in bubbletea v2 (see github.com/charmbracelet/bubbletea issue #162): v2 added
`tea.MouseClickMsg`/`tea.MouseReleaseMsg` and lets a frame's `View()` return a `MouseMode`, so a
program can enable `tea.MouseModeCellMotion` normally and switch to `tea.MouseModeNone` for the
duration of a click-drag, giving back native selection while a button is held and wheel/drag
reporting the rest of the time. That API does not exist in bubbletea v1.3.10 — v1's `Program` has no
per-frame mouse-mode switch and no click/release message split, only the single blanket
`WithMouseCellMotion`/`WithMouseAllMotion` set at startup. Porting the fix means upgrading to
bubbletea v2, which is a larger step than the dependency pin above and needs its own compatibility
pass (lipgloss/x/ansi/x/cellbuf versions, API churn elsewhere in `internal/ui`) before it's worth
doing just for this.

## Testing a TUI

Most behaviour is covered headlessly: build a `Model`, set `m.w/m.h`, call `m.append(lines)` and
assert on `m.View()` through `stripANSI` (`internal/ui/model_test.go`). Prefer this.

For end-to-end checks a `pty.fork` harness works, with three non-obvious requirements: answer the
terminal capability queries bubbletea emits at startup (`\x1b]11;?` → an OSC 11 colour report,
`\x1b[6n` → `\x1b[1;1R`) or it blocks forever waiting; snapshot the output *before* sending `q`,
because teardown emits `\x1b[20;H\x1b[2K` and erases the status line; and feed stdin from a file, not
a pre-filled pipe, which deadlocks past 64 KiB.
