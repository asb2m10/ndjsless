package record

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// maxEmbedDepth caps how far down we keep unwrapping JSON-in-a-string. Logs
// nest twice more often than you would like; four is generous and bounds the
// work on adversarial input.
const maxEmbedDepth = 4

// EmbeddedRule separates a broken line from JSON discovered inside it.
const EmbeddedRule = "--- embedded ---"

// Pretty renders the full record for the detail popup: the message first,
// since it is the one field worth reading, then every other field as a
// fixed two-column table, then any JSON found embedded in a string value.
//
// JSON that was logged as a string gets indented, which is the whole point of
// the popup. There are two shapes of that, and each gets the rendering that
// reads best:
//
//   - the string is nothing but JSON ("message":"{...}"): it is expanded in
//     place, so it indents as real structure;
//   - the string merely contains JSON ("message":"request failed: {...}"): the
//     field keeps its real value, and the fragment is indented in a labelled
//     section underneath. This is the common case for messages.
func Pretty(r Record) string {
	if r.Broken {
		return prettyBroken(r.Raw)
	}
	var parts []string
	if msg, ok := prettyMessage(r.Fields); ok {
		parts = append(parts, msg)
	}
	if tbl := prettyFields(r.Fields); tbl != "" {
		parts = append(parts, tbl)
	}
	parts = append(parts, sections(r.Fields)...)
	return strings.Join(parts, "\n\n")
}

// PrettyMessage renders just the message block of the popup: the field
// itself, indented in place when it is nothing but JSON, plus the section for
// any JSON embedded inside it -- the same content Pretty shows first, without
// the rest of the record. Used so the UI can let the user copy only the
// message, indentation and all, rather than the whole popup.
func PrettyMessage(r Record) string {
	if r.Broken {
		return r.Raw
	}
	var parts []string
	if msg, ok := prettyMessage(r.Fields); ok {
		parts = append(parts, msg)
	}
	for _, s := range sections(r.Fields) {
		if strings.HasPrefix(s, "--- embedded in "+MessageField+" ---") {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}

// prettyMessage renders the message field on its own, ahead of everything
// else, since it is the field actually worth reading.
func prettyMessage(fields map[string]any) (string, bool) {
	v, ok := fields[MessageField]
	if !ok {
		return "", false
	}
	b, err := json.MarshalIndent(expand(v, 0), "", "  ")
	if err != nil {
		// Should not happen: the value round-tripped through Unmarshal already.
		return "", false
	}
	return `"message": ` + string(b), true
}

// prettyFields renders every field but the message as a fixed two-column
// table, one row per field, keys aligned to the widest one. Values are
// flattened to a single line with scalar: this table does not expand nested
// JSON-in-a-string, that is what sections is for.
func prettyFields(fields map[string]any) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		if k == MessageField {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)

	width := 0
	for _, k := range keys {
		if len(k) > width {
			width = len(k)
		}
	}
	rows := make([]string, len(keys))
	for i, k := range keys {
		rows[i] = fmt.Sprintf("%-*s  %s", width, k, scalar(fields[k]))
	}
	return strings.Join(rows, "\n")
}

// HasEmbedded reports whether the record contains JSON embedded in a string,
// so the UI can hint that the popup will show more than the line does.
func HasEmbedded(r Record) bool {
	if r.Broken {
		_, ok := findJSON(r.Raw)
		return ok
	}
	return embedded(r.Fields, 0) || len(sections(r.Fields)) > 0
}

// sections renders one indented block per string field that contains a JSON
// fragment alongside other text. Fields are walked in sorted order so the popup
// is stable between renders.
func sections(v any) []string {
	var out []string
	walkStrings(v, "", 0, func(path, s string) {
		if _, whole := decodeJSONString(s); whole {
			return // already expanded in place
		}
		frag, ok := findJSON(s)
		if !ok {
			return
		}
		var parsed any
		dec := json.NewDecoder(strings.NewReader(frag))
		dec.UseNumber()
		if err := dec.Decode(&parsed); err != nil {
			return
		}
		b, err := json.MarshalIndent(expand(parsed, 0), "", "  ")
		if err != nil {
			return
		}
		out = append(out, "--- embedded in "+path+" ---\n"+string(b))
	})
	return out
}

// walkStrings visits every string in a decoded value, naming it by its dotted
// path, in a deterministic order.
func walkStrings(v any, path string, depth int, fn func(path, s string)) {
	if depth >= maxEmbedDepth {
		return
	}
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walkStrings(t[k], join(path, k), depth+1, fn)
		}
	case []any:
		for i, val := range t {
			walkStrings(val, join(path, strconv.Itoa(i)), depth+1, fn)
		}
	case string:
		if path != "" {
			fn(path, t)
		}
	}
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// prettyBroken shows an unparseable line as-is, then any balanced JSON object or
// array found inside it, indented. This is the "logger not configured yet" case,
// where a framework prefixes the record with its own plain-text banner.
func prettyBroken(raw string) string {
	frag, ok := findJSON(raw)
	if !ok {
		return raw
	}
	var v any
	dec := json.NewDecoder(strings.NewReader(frag))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return raw
	}
	b, err := json.MarshalIndent(expand(v, 0), "", "  ")
	if err != nil {
		return raw
	}
	return raw + "\n\n" + EmbeddedRule + "\n" + string(b)
}

// expand walks a decoded value and replaces every string that is itself JSON
// with its parsed form, so MarshalIndent indents it as structure rather than
// emitting an escaped blob.
func expand(v any, depth int) any {
	if depth >= maxEmbedDepth {
		return v
	}
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = expand(val, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = expand(val, depth+1)
		}
		return out
	case string:
		if inner, ok := decodeJSONString(t); ok {
			return expand(inner, depth+1)
		}
		return t
	default:
		return v
	}
}

// embedded mirrors expand but only reports whether anything would change.
func embedded(v any, depth int) bool {
	if depth >= maxEmbedDepth {
		return false
	}
	switch t := v.(type) {
	case map[string]any:
		for _, val := range t {
			if embedded(val, depth+1) {
				return true
			}
		}
	case []any:
		for _, val := range t {
			if embedded(val, depth+1) {
				return true
			}
		}
	case string:
		_, ok := decodeJSONString(t)
		return ok
	}
	return false
}

// decodeJSONString parses s when it holds a complete JSON object or array. Bare
// scalars are deliberately rejected: turning the string "123" or "null" into a
// number would lose information rather than reveal it.
func decodeJSONString(s string) (any, bool) {
	t := strings.TrimSpace(s)
	if len(t) < 2 {
		return nil, false
	}
	if t[0] != '{' && t[0] != '[' {
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(t))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	// Reject trailing garbage, so "{} oops" stays a plain string.
	if dec.More() {
		return nil, false
	}
	switch v.(type) {
	case map[string]any, []any:
		return v, true
	}
	return nil, false
}

// findJSON locates the first balanced JSON object or array inside raw text,
// ignoring braces that appear inside string literals.
func findJSON(raw string) (string, bool) {
	start := strings.IndexAny(raw, "{[")
	for start >= 0 {
		if end, ok := matchBracket(raw, start); ok {
			frag := raw[start : end+1]
			if json.Valid([]byte(frag)) {
				return frag, true
			}
		}
		next := strings.IndexAny(raw[start+1:], "{[")
		if next < 0 {
			return "", false
		}
		start = start + 1 + next
	}
	return "", false
}

// matchBracket returns the index of the bracket closing the one at start.
func matchBracket(s string, start int) (int, bool) {
	open := s[start]
	close := byte('}')
	if open == '[' {
		close = ']'
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case inStr && c == '\\':
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
			// Brackets inside a string literal do not nest.
		case c == open:
			depth++
		case c == close:
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}
