package record

import (
	"testing"
	"time"
)

const ts = DefaultTsField

func TestParseValid(t *testing.T) {
	r := Parse(`{"eventTimestamp":"2026-10-01T09:58:01.120Z","level":"info","message":"hello","port":8080}`, ts)
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
		`{"eventTimestamp":"2026-10-01T09:58:01Z","message":"truncated`,
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
	r := Parse(`{"eventTimestamp":"not-a-timestamp","message":"x"}`, ts)
	if got := r.Timestamp(); got != "not-a-timestamp" {
		t.Errorf("Timestamp() = %q, want the raw value", got)
	}
}

func TestEpochSecondsVersusMillis(t *testing.T) {
	sec := Parse(`{"eventTimestamp":1759305488,"message":"x"}`, ts)
	mil := Parse(`{"eventTimestamp":1759305488000,"message":"x"}`, ts)
	if !sec.Ts.Equal(mil.Ts) {
		t.Errorf("epoch seconds %v and millis %v should be the same instant", sec.Ts, mil.Ts)
	}
	if sec.Ts.Year() != 2025 {
		t.Errorf("epoch seconds parsed to year %d, want 2025", sec.Ts.Year())
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
