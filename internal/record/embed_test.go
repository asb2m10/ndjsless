package record

import (
	"strings"
	"testing"
)

func TestPrettyIndentsEmbeddedJSON(t *testing.T) {
	r := Parse(`{"message":"request failed: ignored","detail":"{\"status\":502,\"upstream\":\"payments\"}"}`, ts)
	out := Pretty(r)
	if strings.Contains(out, `\"status\"`) {
		t.Errorf("embedded JSON left escaped:\n%s", out)
	}
	if !strings.Contains(out, `"status": 502`) {
		t.Errorf("embedded JSON not indented:\n%s", out)
	}
	if !HasEmbedded(r) {
		t.Error("HasEmbedded = false, want true")
	}
}

func TestPrettyIndentsTwoLevelsDeep(t *testing.T) {
	r := Parse(`{"message":"x","payload":"{\"nested\":\"{\\\"deep\\\":[1,2]}\"}"}`, ts)
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
	for _, want := range []string{`"count": "123"`, `"empty": "null"`, `not json {at all`} {
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
	if got := Pretty(r); !strings.Contains(got, `"{\"a\":1} oops"`) {
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
	r := Parse(`{"eventTimestamp":"2026-10-01T09:58:04.771Z","message":"request failed: {\"status\":502,\"detail\":{\"code\":\"ETIMEDOUT\",\"attempts\":[1,2,3]}}"}`, ts)
	out := Pretty(r)
	if !strings.Contains(out, "--- embedded in message ---") {
		t.Fatalf("no section for the JSON embedded in message:\n%s", out)
	}
	if !strings.Contains(out, `"status": 502`) || !strings.Contains(out, `"code": "ETIMEDOUT"`) {
		t.Errorf("embedded JSON not indented:\n%s", out)
	}
	// The field itself must still be shown as it really was.
	if !strings.Contains(out, `"message": "request failed:`) {
		t.Errorf("the real field value was dropped:\n%s", out)
	}
	if !HasEmbedded(r) {
		t.Error("HasEmbedded = false, want true")
	}
}

func TestNoSectionWhenTheStringIsEntirelyJSON(t *testing.T) {
	// Expanded in place, so a duplicate section would be noise.
	r := Parse(`{"message":"x","payload":"{\"a\":1}"}`, ts)
	out := Pretty(r)
	if strings.Contains(out, "--- embedded in") {
		t.Errorf("whole-string JSON should be expanded in place, not sectioned:\n%s", out)
	}
	if !strings.Contains(out, `"a": 1`) {
		t.Errorf("whole-string JSON not expanded:\n%s", out)
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
