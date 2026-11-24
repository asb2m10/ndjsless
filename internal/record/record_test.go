package record

import (
	"testing"
	"time"
	"unsafe"
)

const ts = DefaultTsField

func TestParseValid(t *testing.T) {
	r := Parse(`{"eventTime":"2026-10-01T09:58:01.120Z","level":"info","message":"hello","port":8080}`, ts)
	if r.Broken {
		t.Fatal("valid line marked broken")
	}
	if got := r.Column("message"); got != "hello" {
		t.Errorf("message = %q, want %q", got, "hello")
	}
	if got := r.Column("port"); got != "8080" {
		t.Errorf("port = %q, want %q (json.Number must not become 8.08e+03)", got, "8080")
	}
	if r.Level != "info" {
		t.Errorf("level = %q, want info", r.Level)
	}
	if want := time.Date(2026, 10, 1, 9, 58, 1, 120e6, time.UTC); !r.Ts.Equal(want) {
		t.Errorf("Ts = %v, want %v", r.Ts, want)
	}
}

func TestParseBroken(t *testing.T) {
	for _, line := range []string{
		"Starting application v1.2.3",
		`{"eventTime":"2026-10-01T09:58:01Z","message":"truncated`,
		`[1,2,3]`, // valid JSON, but not a record
		"",
	} {
		r := Parse(line, ts)
		if !r.Broken {
			t.Errorf("Parse(%q) should be broken", line)
		}
		if got := r.Column("message"); got != line {
			t.Errorf("Parse(%q).Column(message) = %q, want the raw line", line, got)
		}
		if got := r.Column("level"); got != "" {
			t.Errorf("Parse(%q).Column(level) = %q, want empty", line, got)
		}
	}
}

func TestMissingTimestamp(t *testing.T) {
	r := Parse(`{"message":"no ts"}`, ts)
	if !r.Ts.IsZero() || r.Timestamp() != "" {
		t.Errorf("Timestamp() = %q, want empty", r.Timestamp())
	}
}

func TestUnparseableTimestampShownRaw(t *testing.T) {
	r := Parse(`{"eventTime":"not-a-timestamp","message":"x"}`, ts)
	if got := r.Timestamp(); got != "not-a-timestamp" {
		t.Errorf("Timestamp() = %q, want the raw value", got)
	}
}

func TestEpochSecondsVersusMillis(t *testing.T) {
	sec := Parse(`{"eventTime":1759305488,"message":"x"}`, ts)
	mil := Parse(`{"eventTime":1759305488000,"message":"x"}`, ts)
	if !sec.Ts.Equal(mil.Ts) {
		t.Errorf("epoch seconds %v and millis %v should be the same instant", sec.Ts, mil.Ts)
	}
	if sec.Ts.Year() != 2025 {
		t.Errorf("epoch seconds parsed to year %d, want 2025", sec.Ts.Year())
	}
}

func TestEpochMillisAsJSString(t *testing.T) {
	// JS loses precision on integers past 2^53, so a logger that formats its
	// timestamp in JavaScript sends epoch millis as a string instead of a
	// number to keep it exact.
	str := Parse(`{"eventTime":"1759305488000","message":"x"}`, ts)
	num := Parse(`{"eventTime":1759305488000,"message":"x"}`, ts)
	if !str.Ts.Equal(num.Ts) {
		t.Errorf("string millis %v should equal numeric millis %v", str.Ts, num.Ts)
	}
	if str.Ts.Year() != 2025 {
		t.Errorf("parsed to year %d, want 2025", str.Ts.Year())
	}
}

func TestNestedFieldPath(t *testing.T) {
	r := Parse(`{"message":"x","http":{"status":500,"method":"POST"}}`, ts)
	if got := r.Column("http.status"); got != "500" {
		t.Errorf("http.status = %q, want 500", got)
	}
	if got := r.Column("http.missing"); got != "" {
		t.Errorf("http.missing = %q, want empty", got)
	}
	// A nested object as a whole column must stay on one line.
	if got := r.Column("http"); got != `{"method":"POST","status":500}` {
		t.Errorf("http = %q, want compact JSON", got)
	}
}

func TestFlatDottedKeyPreferredOverPath(t *testing.T) {
	r := Parse(`{"log.level":"warn","message":"x"}`, ts)
	if r.Level != "warn" {
		t.Errorf("Level = %q, want warn", r.Level)
	}
}

func TestLevelAliases(t *testing.T) {
	cases := map[string]string{
		`{"severity":"ERROR"}`: "error",
		`{"lvl":"warning"}`:    "warn",
		`{"level":30}`:         "info",
		`{"level":50}`:         "error",
		`{"level":"panic"}`:    "fatal",
		`{"message":"x"}`:      "",
	}
	for line, want := range cases {
		if got := Parse(line, ts).Level; got != want {
			t.Errorf("Parse(%s).Level = %q, want %q", line, got, want)
		}
	}
}

func TestCustomTsField(t *testing.T) {
	r := Parse(`{"time":"2026-10-01T09:58:01Z","message":"x"}`, "time")
	if r.Ts.IsZero() {
		t.Error("custom -ts-field not honoured")
	}
}

func TestTimeFieldIsTheTimestampFallback(t *testing.T) {
	r := Parse(`{"time":"2026-10-01T09:58:01Z","message":"x"}`, ts)
	if want := time.Date(2026, 10, 1, 9, 58, 1, 0, time.UTC); !r.Ts.Equal(want) {
		t.Errorf("Ts = %v, want %v from the time field", r.Ts, want)
	}
	r = Parse(`{"eventTime":"2026-10-01T09:58:01Z","time":"2020-01-01T00:00:00Z"}`, ts)
	if r.Ts.Year() != 2026 {
		t.Errorf("Ts = %v, the configured field must win over time", r.Ts)
	}
}

func TestMessageFallsBackToMsgThenRawLine(t *testing.T) {
	if got := Parse(`{"msg":"hi","message":"hello"}`, ts).Column("message"); got != "hello" {
		t.Errorf("message = %q, want message to win over msg", got)
	}
	if got := Parse(`{"msg":"hi"}`, ts).Column("message"); got != "hi" {
		t.Errorf("message = %q, want msg as the fallback", got)
	}
	line := `{"level":"info","port":8080}`
	if got := Parse(line, ts).Column("message"); got != line {
		t.Errorf("message = %q, want the raw line", got)
	}
}

func TestCompactRecordAnswersLikeTheParsedOne(t *testing.T) {
	line := `{"eventTime":"2026-10-01T09:58:01.120Z","message":"failed: {\"code\":1}","http":{"status":502}}`
	full := Parse(line, ts)
	c := full.Compact()
	if c.Fields() == nil {
		t.Fatal("a compact record lost its fields")
	}
	for _, name := range []string{"message", "http.status", "missing"} {
		if got, want := c.Column(name), full.Column(name); got != want {
			t.Errorf("Column(%q) = %q compact, %q parsed", name, got, want)
		}
		if got, want := c.Has(name), full.Has(name); got != want {
			t.Errorf("Has(%q) = %v compact, %v parsed", name, got, want)
		}
	}
	if Pretty(c) != Pretty(full) || HasEmbedded(c) != HasEmbedded(full) {
		t.Error("the popup differs between a compact and a parsed record")
	}
}

func TestCompactBrokenRecordStaysBroken(t *testing.T) {
	c := Parse("plain text", ts).Compact()
	if c.Fields() != nil || c.Column("message") != "plain text" {
		t.Errorf("broken compact record: fields %v, message %q", c.Fields(), c.Column("message"))
	}
}

func TestCellSharesTheLineWhenUnescaped(t *testing.T) {
	line := `{"message":"hello world","note":"a\nb"}`
	r := Parse(line, ts)
	if got := r.Cell("message"); got != "hello world" || !within(line, got) {
		t.Errorf("Cell(message) = %q, want a substring of the line", got)
	}
	// An escaped value cannot be a substring; it must still come back decoded.
	if got := r.Cell("note"); got != "a\nb" {
		t.Errorf("Cell(note) = %q, want the decoded value", got)
	}
}

// within reports whether sub points into s's memory.
func within(s, sub string) bool {
	if len(sub) == 0 {
		return false
	}
	start := uintptr(unsafe.Pointer(unsafe.StringData(s)))
	p := uintptr(unsafe.Pointer(unsafe.StringData(sub)))
	return p >= start && p+uintptr(len(sub)) <= start+uintptr(len(s))
}

func TestDetectsISOTimestampInUnknownField(t *testing.T) {
	r := Parse(`{"message":"x","logged_at":"2026-11-06T19:05:00.848833+00:00"}`, "")
	if r.Ts.IsZero() || r.Ts.UTC().Format("15:04:05.000") != "19:05:00.848" {
		t.Fatalf("not detected: %v", r.Ts)
	}
	r = Parse(`{"timestamp":"2026-11-06T19:05:00.848833+00:00","other":"2026-01-01T00:00:00Z"}`, "")
	if r.Ts.Year() != 2026 || r.Ts.Month() != 11 {
		t.Fatalf("well-known name should win: %v", r.Ts)
	}
	if r := Parse(`{"message":"2026-11-06 is not a time T"}`, ""); !r.Ts.IsZero() {
		t.Fatal("false positive")
	}
}
