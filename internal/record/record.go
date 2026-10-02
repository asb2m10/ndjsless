// Package record turns ndjson log lines into something a pager can lay out in
// columns. Lines that are not valid JSON are kept verbatim rather than dropped:
// services routinely print a few plain lines before their logger is configured.
package record

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DefaultTsField is the timestamp field, overridable with --ts-field.
const DefaultTsField = "eventTimestamp"

// MessageField is the field rendered as the log message.
const MessageField = "message"

// Record is one parsed log line.
type Record struct {
	Raw    string         // the original line, verbatim
	Fields map[string]any // nil when the line is not valid JSON
	Broken bool           // true => render Raw as the message
	Ts     time.Time      // zero if absent or unparseable
	TsRaw  string         // fallback text for the timestamp column
	Level  string         // normalised lowercase level, "" if none
}

// levelFields are consulted in order; the first one present wins.
var levelFields = []string{"level", "severity", "lvl", "log.level"}

// syslogLevels maps numeric severities, as emitted by syslog and by bunyan-style
// loggers, onto names.
var syslogLevels = map[int]string{
	0: "error", 1: "error", 2: "error", 3: "error", // emerg..err
	4: "warn", 5: "info", 6: "info", 7: "debug",
	10: "trace", 20: "debug", 30: "info", 40: "warn", 50: "error", 60: "fatal",
}

// Parse decodes a single line. It never fails: an undecodable line comes back
// with Broken set.
func Parse(line string, tsField string) Record {
	r := Record{Raw: line}
	if tsField == "" {
		tsField = DefaultTsField
	}

	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "{") {
		// Cheap rejection: a log record is an object. Arrays and bare scalars
		// are not records even though they are valid JSON.
		r.Broken = true
		return r
	}

	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber() // keep epoch millis and large ids exact
	var fields map[string]any
	if err := dec.Decode(&fields); err != nil || fields == nil {
		r.Broken = true
		return r
	}
	r.Fields = fields

	if v, ok := lookup(fields, tsField); ok {
		r.Ts, r.TsRaw = parseTime(v)
	}
	for _, name := range levelFields {
		if v, ok := lookup(fields, name); ok {
			if lv := normaliseLevel(v); lv != "" {
				r.Level = lv
				break
			}
		}
	}
	return r
}

// Column renders one field as a single line of text. The name may be a dotted
// path ("http.status") to descend into nested objects.
func (r Record) Column(name string) string {
	if r.Broken {
		// The whole line is the message; every other column is blank.
		if name == MessageField {
			return r.Raw
		}
		return ""
	}
	v, ok := lookup(r.Fields, name)
	if !ok {
		return ""
	}
	return scalar(v)
}

// Timestamp renders the timestamp column: local HH:MM:SS.mmm when the value
// parsed, otherwise whatever raw text was there.
func (r Record) Timestamp() string {
	if !r.Ts.IsZero() {
		return r.Ts.Local().Format("15:04:05.000")
	}
	return r.TsRaw
}

// lookup resolves a dotted path. A literal key containing dots is preferred
// over the path interpretation, since some loggers emit "log.level" flat.
func lookup(fields map[string]any, path string) (any, bool) {
	if v, ok := fields[path]; ok {
		return v, true
	}
	if !strings.Contains(path, ".") {
		return nil, false
	}
	var cur any = fields
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// scalar flattens a JSON value to one line. Objects and arrays are re-marshalled
// compactly so that they never break the column layout.
func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(bytes.ReplaceAll(b, []byte("\n"), []byte(" ")))
	}
}

// timeLayouts are tried in order for string timestamps.
var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.000Z0700",
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999",
	"2006-01-02 15:04:05",
	time.RFC1123Z,
	time.RFC1123,
	time.Stamp,
}

// epochMillisCutoff separates epoch seconds from epoch milliseconds. Seconds
// stay below it until the year 5138, millis exceed it after 1973.
const epochMillisCutoff = 1e11

// parseTime returns the parsed instant plus the raw text to show if parsing
// failed.
func parseTime(v any) (time.Time, string) {
	switch t := v.(type) {
	case string:
		for _, layout := range timeLayouts {
			if ts, err := time.Parse(layout, t); err == nil {
				return ts, t
			}
		}
		return time.Time{}, t
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return fromEpoch(f), t.String()
		}
		return time.Time{}, t.String()
	case float64:
		return fromEpoch(t), scalar(v)
	default:
		return time.Time{}, scalar(v)
	}
}

func fromEpoch(f float64) time.Time {
	if f == 0 {
		return time.Time{}
	}
	if abs(f) < epochMillisCutoff {
		sec, frac := splitFloat(f)
		return time.Unix(sec, int64(frac*float64(time.Second)))
	}
	return time.UnixMilli(int64(f))
}

func splitFloat(f float64) (int64, float64) {
	sec := int64(f)
	return sec, f - float64(sec)
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// normaliseLevel maps a level value onto a lowercase canonical name.
func normaliseLevel(v any) string {
	switch t := v.(type) {
	case string:
		return canonLevel(strings.ToLower(strings.TrimSpace(t)))
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return syslogLevels[int(n)]
		}
	case float64:
		return syslogLevels[int(t)]
	}
	return ""
}

func canonLevel(s string) string {
	switch s {
	case "err", "error", "eror":
		return "error"
	case "crit", "critical", "fatal", "panic", "emerg", "alert":
		return "fatal"
	case "warn", "warning", "warne":
		return "warn"
	case "info", "notice", "information":
		return "info"
	case "debug", "dbg":
		return "debug"
	case "trace", "verbose":
		return "trace"
	}
	// Unknown but non-empty: keep it, it still deserves a column.
	if s == "" {
		return ""
	}
	// A numeric level arriving as a string.
	if n, err := strconv.Atoi(s); err == nil {
		return syslogLevels[n]
	}
	return s
}
