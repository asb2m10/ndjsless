package record

import (
	"strings"
	"testing"
)

func TestPrettyShowsWholeJSONFieldCompactInTheTable(t *testing.T) {
	// A field that is nothing but JSON is only expanded in place when it is
	// the message; any other field gets one row in the fixed table instead,
	// with real characters rather than escaped ones.
	r := Parse(`{"message":"request failed: ignored","detail":"{\"status\":502,\"upstream\":\"payments\"}"}`, ts)
	out := Pretty(r)
	if strings.Contains(out, `\"status\"`) {
		t.Errorf("field value left escaped:\n%s", out)
	}
	if !strings.Contains(out, `{"status":502,"upstream":"payments"}`) {
		t.Errorf("detail field missing from the table:\n%s", out)
	}
	if !HasEmbedded(r) {
		t.Error("HasEmbedded = false, want true")
	}
}

func TestPrettyIndentsTwoLevelsDeepInTheMessage(t *testing.T) {
	r := Parse(`{"message":"{\"nested\":\"{\\\"deep\\\":[1,2]}\"}"}`, ts)
	out := Pretty(r)
	if !strings.Contains(out, `"deep"`) {
		t.Errorf("second level not unwrapped:\n%s", out)
	}
	if strings.Contains(out, `\\\"deep\\\"`) {
		t.Errorf("second level left escaped:\n%s", out)
	}
}

func TestPrettyLeavesPlainStringsAlone(t *testing.T) {
	r := Parse(`{"message":"not json {at all","count":"123","empty":"null"}`, ts)
	out := Pretty(r)
	for _, want := range []string{"count", "123", "empty", "null", `not json {at all`} {
		if !strings.Contains(out, want) {
			t.Errorf("Pretty() missing %q:\n%s", want, out)
		}
	}
	if HasEmbedded(r) {
		t.Error("HasEmbedded = true for a record with no embedded JSON")
	}
}

func TestPrettyRejectsTrailingGarbage(t *testing.T) {
	r := Parse(`{"message":"x","f":"{\"a\":1} oops"}`, ts)
	if got := Pretty(r); !strings.Contains(got, `{"a":1} oops`) {
		t.Errorf("string with trailing garbage should stay a string:\n%s", got)
	}
}

func TestPrettyBrokenLineWithEmbeddedJSON(t *testing.T) {
	raw := `[main] WARN  startup - config was {"retries":3,"hosts":["a","b"]} at boot`
	r := Parse(raw, ts)
	if !r.Broken {
		t.Fatal("line should be broken")
	}
	out := Pretty(r)
	if !strings.HasPrefix(out, raw) {
		t.Errorf("broken line should be shown verbatim first:\n%s", out)
	}
	if !strings.Contains(out, EmbeddedRule) {
		t.Errorf("missing embedded rule:\n%s", out)
	}
	if !strings.Contains(out, `"retries": 3`) {
		t.Errorf("embedded JSON not indented:\n%s", out)
	}
	if !HasEmbedded(r) {
		t.Error("HasEmbedded = false for a broken line containing JSON")
	}
}

func TestPrettyBrokenLineWithoutJSON(t *testing.T) {
	raw := "Starting application v1.2.3"
	if got := Pretty(Parse(raw, ts)); got != raw {
		t.Errorf("Pretty() = %q, want the raw line unchanged", got)
	}
}

func TestFindJSONIgnoresBracesInStrings(t *testing.T) {
	// The brace inside the string literal must not confuse the matcher.
	raw := `prefix {"msg":"a } brace","n":1} suffix`
	frag, ok := findJSON(raw)
	if !ok {
		t.Fatal("findJSON failed")
	}
	if frag != `{"msg":"a } brace","n":1}` {
		t.Errorf("findJSON = %q", frag)
	}
}

func TestPrettyIsStable(t *testing.T) {
	r := Parse(`{"b":1,"a":2,"c":3,"message":"x"}`, ts)
	if Pretty(r) != Pretty(r) {
		t.Error("Pretty() is not deterministic; map key order must be sorted")
	}
}

func TestPrettyIndentsJSONEmbeddedInAMessage(t *testing.T) {
	// The common shape: a human-readable prefix followed by a JSON payload.
	r := Parse(`{"eventTime":"2026-10-01T09:58:04.771Z","message":"request failed: {\"status\":502,\"detail\":{\"code\":\"ETIMEDOUT\",\"attempts\":[1,2,3]}}"}`, ts)
	out := Pretty(r)
	if !strings.HasPrefix(out, "--- embedded in message ---") {
		t.Fatalf("section for the JSON embedded in message must come first:\n%s", out)
	}
	if !strings.Contains(out, `"status": 502`) || !strings.Contains(out, `"code": "ETIMEDOUT"`) {
		t.Errorf("embedded JSON not indented:\n%s", out)
	}
	// The plain field would just repeat the same JSON escaped, so it is
	// dropped once its embedded section is shown.
	if strings.Contains(out, `"message": "request failed:`) {
		t.Errorf("the plain field duplicates the embedded section:\n%s", out)
	}
	if !HasEmbedded(r) {
		t.Error("HasEmbedded = false, want true")
	}
}

func TestNoSectionWhenTheStringIsEntirelyJSON(t *testing.T) {
	// A duplicate section would be noise: the field's own table row already
	// shows the value, just not indented, since the table is one row per field.
	r := Parse(`{"message":"x","payload":"{\"a\":1}"}`, ts)
	out := Pretty(r)
	if strings.Contains(out, "--- embedded in") {
		t.Errorf("whole-string JSON should not be sectioned:\n%s", out)
	}
	if !strings.Contains(out, `{"a":1}`) {
		t.Errorf("field value missing from the table:\n%s", out)
	}
}

func TestPrettyMessageFirstThenFieldTable(t *testing.T) {
	r := Parse(`{"message":"hello","level":"info","port":8080}`, ts)
	out := Pretty(r)
	msgIdx := strings.Index(out, `"message": "hello"`)
	if msgIdx != 0 {
		t.Fatalf("message must come first:\n%s", out)
	}
	if strings.Contains(out, "\"message\"") && strings.Count(out, "message") > 1 {
		t.Errorf("message field leaked into the field table:\n%s", out)
	}
	levelIdx := strings.Index(out, "level")
	portIdx := strings.Index(out, "port")
	if levelIdx < msgIdx || portIdx < msgIdx {
		t.Errorf("fields must come after the message:\n%s", out)
	}
	if !strings.Contains(out, "8080") {
		t.Errorf("port missing from the field table:\n%s", out)
	}
}

func TestSectionsAreDeterministic(t *testing.T) {
	r := Parse(`{"a":"see {\"x\":1}","b":"see {\"y\":2}","c":"see {\"z\":3}","message":"m"}`, ts)
	first := Pretty(r)
	for i := 0; i < 20; i++ {
		if Pretty(r) != first {
			t.Fatal("Pretty() section order is not stable")
		}
	}
	if n := strings.Count(first, "--- embedded in "); n != 3 {
		t.Errorf("got %d sections, want 3", n)
	}
	ia := strings.Index(first, "embedded in a")
	ib := strings.Index(first, "embedded in b")
	if ia < 0 || ib < ia {
		t.Errorf("sections not in sorted order:\n%s", first)
	}
}

func TestNoSectionForPlainText(t *testing.T) {
	r := Parse(`{"message":"nothing json here at all {not balanced"}`, ts)
	if out := Pretty(r); strings.Contains(out, "--- embedded in") {
		t.Errorf("spurious section:\n%s", out)
	}
}

func TestNestedFieldPathInSectionLabel(t *testing.T) {
	r := Parse(`{"message":"m","err":{"cause":"boom {\"n\":1}"}}`, ts)
	if out := Pretty(r); !strings.Contains(out, "--- embedded in err.cause ---") {
		t.Errorf("section not labelled with the dotted path:\n%s", out)
	}
}
