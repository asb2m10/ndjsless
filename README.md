# ndjsless

A `less` for ndjson logs.

Application logs are ndjson, but `less` shows you the raw JSON — one enormous
line per entry — and `jq` pretty-prints without being able to page, follow or
search. `ndjsless` does what `less` does, over records instead of lines.

```
tail -f app.log | ndjsless
ndjsless app.log
ndjsless -f app.log
```

By default it shows two columns, `eventTimestamp` and `message`, because those
are the two fields you actually read. Everything else is one keypress away:
press <kbd>Enter</kbd> and the whole record opens in a popup, pretty-printed.

```
              Starting application v1.2.3 (logger not yet configured)
              2026-10-01 09:58:00 INFO  bootstrapping config from /etc/app/con
05:58:01.120  http server listening
05:58:01.457  route table built
05:58:02.004  queue consumer started
03:58:03.120  upstream slow, retrying
05:58:04.771  request failed: {"status":502,"upstream":"payments","detail":{"c
05:58:05.002  this is a deliberately very long message intended to overflow th
testdata/sample.ndjson · 15 lines · END · ⏎ json  (? for help)
```

The first two lines are not JSON, so they are shown as-is. `⏎ json` in the
status bar means the selected record has JSON inside it that <kbd>Enter</kbd>
will indent. A column header appears once you ask for more than the two default
columns.

## What it handles

**Broken JSON.** A service prints a few plain lines before its logger is
configured. Those are shown verbatim as the message, dimmed, rather than
dropped. If such a line happens to contain a JSON fragment, the popup indents
that too.

**JSON logged as a string.** Loggers constantly do
`log.error("request failed: " + JSON.stringify(err))`. The popup finds that
fragment and indents it:

```
{
  "eventTimestamp": "2026-10-01T09:58:04.771Z",
  "level": "error",
  "message": "request failed: {\"status\":502,\"detail\":{\"code\":\"ETIMEDOUT\"}}"
}

--- embedded in message ---
{
  "detail": {
    "code": "ETIMEDOUT"
  },
  "status": 502
}
```

A field whose value is *nothing but* JSON is expanded in place instead, since
there is no surrounding text to preserve, and JSON nested inside that is
unwrapped too. Records nested more than four objects deep are left alone.

**No word wrap.** A row is always one record. Long lines are scrolled sideways
with <kbd>h</kbd>/<kbd>l</kbd>, and wrapping happens only inside the popup.

**Follow mode.** Piped input starts pinned to the newest record. Scroll up and
the view holds still while the log keeps growing; <kbd>G</kbd> resumes.

**Big records.** Only the cells that can appear on screen are ever rendered, so
a megabyte-long record pages as fast as a short one.

## Keys

| Key | |
|---|---|
| <kbd>j</kbd> <kbd>k</kbd> <kbd>↓</kbd> <kbd>↑</kbd> | line down / up |
| <kbd>d</kbd> <kbd>u</kbd> | half page down / up |
| <kbd>f</kbd> <kbd>b</kbd> <kbd>space</kbd> <kbd>PgDn</kbd> | full page down / up |
| <kbd>g</kbd> <kbd>G</kbd> | first / last record (<kbd>G</kbd> resumes follow) |
| <kbd>h</kbd> <kbd>l</kbd> <kbd>←</kbd> <kbd>→</kbd> | scroll sideways |
| <kbd>0</kbd> <kbd>$</kbd> | line start / furthest right |
| <kbd>/</kbd> <kbd>n</kbd> <kbd>N</kbd> | search, next, previous |
| <kbd>Enter</kbd> | full record, embedded JSON indented |
| <kbd>F</kbd> | toggle follow |
| <kbd>?</kbd> | help |
| <kbd>q</kbd> | quit (or close the popup) |

Search is a case-insensitive substring match over the rendered columns, so it
matches what you can see rather than the raw JSON.

## Flags

| Flag | |
|---|---|
| `-fields a,b,c` | show these columns instead of the defaults |
| `-add a,b` | add these columns to the defaults, message stays last |
| `-ts-field name` | the timestamp field (default `eventTimestamp`) |
| `-f`, `-follow` | keep reading as the file grows |
| `-no-color` | monochrome; `NO_COLOR` works too |

Field names may be dotted paths: `-add http.status`. Timestamps are parsed from
RFC 3339, a few common layouts, or epoch seconds/milliseconds, and rendered as
local `HH:MM:SS.mmm`; anything unparseable is shown as-is.

## Install

```sh
go install github.com/asb2m10/ndjsless/cmd/ndjsless@latest
```

## Notes

Keys are read from `/dev/tty`, not stdin, which is what lets the log arrive on
stdin. Consequently `ndjsless` needs a terminal and will say so if it has none.
