package ui

import (
	"strings"
	"testing"

	"github.com/asb2m10/ndjsless/internal/record"
	tea "github.com/charmbracelet/bubbletea"
)

// newTestModel builds a model of a fixed size over the given lines, with colour
// off so the assertions can compare plain text.
func newTestModel(t *testing.T, w, h int, cols []string, lines ...string) Model {
	t.Helper()
	ch := make(chan string)
	m := New(Config{Columns: cols, TsField: record.DefaultTsField, Title: "test"}, ch)
	m.w, m.h = w, h
	m.append(lines)
	return m
}

func defaultCols() []string {
	return []string{record.DefaultTsField, record.MessageField}
}

// rows returns the record rows of the rendered view, without header or status.
func rows(m Model) []string {
	lines := strings.Split(m.View(), "\n")
	if m.showHeader() {
		lines = lines[1:]
	}
	return lines[:len(lines)-1]
}

func TestViewHasExactlyScreenHeightLines(t *testing.T) {
	m := newTestModel(t, 80, 10, defaultCols(),
		`{"eventTimestamp":"2026-10-01T09:58:01.120Z","message":"one"}`,
		`{"eventTimestamp":"2026-10-01T09:58:02.120Z","message":"two"}`,
	)
	if got := len(strings.Split(m.View(), "\n")); got != 10 {
		t.Errorf("view has %d lines, want 10 (the terminal height)", got)
	}
}

func TestRowsNeverExceedTerminalWidth(t *testing.T) {
	long := strings.Repeat("abcdefghij ", 40)
	m := newTestModel(t, 40, 6, defaultCols(),
		`{"eventTimestamp":"2026-10-01T09:58:01.120Z","message":"`+long+`"}`)
	for i, line := range strings.Split(m.View(), "\n") {
		if n := len([]rune(stripANSI(line))); n > 40 {
			t.Errorf("line %d is %d cells wide, want <= 40: %q", i, n, line)
		}
	}
}

func TestNoWordWrapOneRecordPerRow(t *testing.T) {
	long := strings.Repeat("x", 500)
	m := newTestModel(t, 40, 6, defaultCols(),
		`{"eventTimestamp":"2026-10-01T09:58:01.120Z","message":"`+long+`"}`,
		`{"eventTimestamp":"2026-10-01T09:58:02.120Z","message":"second"}`)
	r := rows(m)
	if !strings.Contains(r[1], "second") {
		t.Errorf("the long record wrapped onto a second row; row 1 = %q", r[1])
	}
}

func TestEmbeddedNewlinesDoNotBreakTheRow(t *testing.T) {
	m := newTestModel(t, 60, 6, defaultCols(),
		`{"eventTimestamp":"2026-10-01T09:58:01.120Z","message":"a\nb\tc"}`,
		`{"eventTimestamp":"2026-10-01T09:58:02.120Z","message":"second"}`)
	if got := len(strings.Split(m.View(), "\n")); got != 6 {
		t.Errorf("a newline inside a message added rows: view has %d lines, want 6", got)
	}
	if !strings.Contains(rows(m)[1], "second") {
		t.Errorf("row 1 = %q, want the second record", rows(m)[1])
	}
}

func TestHorizontalScroll(t *testing.T) {
	m := newTestModel(t, 30, 6, defaultCols(),
		`{"eventTimestamp":"2026-10-01T09:58:01.120Z","message":"HEAD`+strings.Repeat("-", 60)+`TAIL"}`)
	if !strings.Contains(rows(m)[0], "HEAD") {
		t.Fatal("expected the start of the line at xoff 0")
	}
	m.xoff = m.maxXOff()
	if !strings.Contains(rows(m)[0], "TAIL") {
		t.Errorf("$ did not reach the end of the line: %q", rows(m)[0])
	}
	if strings.Contains(rows(m)[0], "HEAD") {
		t.Errorf("scrolled right but the start is still visible: %q", rows(m)[0])
	}
}

func TestBrokenLineRendersAsMessage(t *testing.T) {
	m := newTestModel(t, 80, 6, defaultCols(), "Starting application v1.2.3")
	if !strings.Contains(rows(m)[0], "Starting application v1.2.3") {
		t.Errorf("broken line not shown: %q", rows(m)[0])
	}
}

func TestFieldsReplaceDefaults(t *testing.T) {
	m := newTestModel(t, 80, 6, []string{"level", "service", "message"},
		`{"eventTimestamp":"2026-10-01T09:58:01.120Z","level":"info","service":"api","message":"up"}`)
	row := rows(m)[0]
	if strings.Contains(row, "09:58") {
		t.Errorf("timestamp shown although -fields replaced it: %q", row)
	}
	for _, want := range []string{"info", "api", "up"} {
		if !strings.Contains(row, want) {
			t.Errorf("row %q missing %q", row, want)
		}
	}
	if !strings.Contains(m.View(), "LEVEL") {
		t.Error("header missing when extra columns are configured")
	}
}

func TestColumnWidthsOnlyGrow(t *testing.T) {
	m := newTestModel(t, 80, 6, []string{"service", "message"},
		`{"service":"averyverylongservicename","message":"a"}`)
	before := m.cols[0].width
	m.append([]string{`{"service":"x","message":"b"}`})
	if m.cols[0].width != before {
		t.Errorf("column width shrank from %d to %d; the view would jitter", before, m.cols[0].width)
	}
}

func TestColumnWidthIsClamped(t *testing.T) {
	m := newTestModel(t, 200, 6, []string{"service", "message"},
		`{"service":"`+strings.Repeat("s", 100)+`","message":"a"}`)
	if m.cols[0].width != colClamp {
		t.Errorf("column width = %d, want it clamped to %d", m.cols[0].width, colClamp)
	}
}

func TestSearchJumpsAndCounts(t *testing.T) {
	lines := []string{
		`{"eventTimestamp":"2026-10-01T09:58:01.000Z","message":"alpha"}`,
		`{"eventTimestamp":"2026-10-01T09:58:02.000Z","message":"needle one"}`,
		`{"eventTimestamp":"2026-10-01T09:58:03.000Z","message":"beta"}`,
		`{"eventTimestamp":"2026-10-01T09:58:04.000Z","message":"NEEDLE two"}`,
	}
	m := newTestModel(t, 80, 10, defaultCols(), lines...)
	m.cursor = 0
	m.setSearch("needle")

	if len(m.matches) != 2 {
		t.Fatalf("matches = %v, want 2 (search must be case-insensitive)", m.matches)
	}
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (first match at or after the cursor)", m.cursor)
	}
	m.jumpMatch(1)
	if m.cursor != 3 {
		t.Errorf("after n, cursor = %d, want 3", m.cursor)
	}
	m.jumpMatch(1) // wraps
	if m.cursor != 1 {
		t.Errorf("n at the last match should wrap to 1, got %d", m.cursor)
	}
	m.jumpMatch(-1)
	if m.cursor != 3 {
		t.Errorf("N at the first match should wrap to 3, got %d", m.cursor)
	}
}

func TestSearchNotFound(t *testing.T) {
	m := newTestModel(t, 80, 10, defaultCols(),
		`{"eventTimestamp":"2026-10-01T09:58:01.000Z","message":"alpha"}`)
	m.setSearch("zzz")
	if !strings.Contains(m.status, "Pattern not found") {
		t.Errorf("status = %q, want a not-found message", m.status)
	}
}

func TestSearchMatchesNewRecordsAsTheyArrive(t *testing.T) {
	m := newTestModel(t, 80, 10, defaultCols(),
		`{"eventTimestamp":"2026-10-01T09:58:01.000Z","message":"needle"}`)
	m.setSearch("needle")
	m.append([]string{`{"eventTimestamp":"2026-10-01T09:58:02.000Z","message":"needle again"}`})
	if len(m.matches) != 2 {
		t.Errorf("matches = %v, want streaming records to be matched too", m.matches)
	}
}

func TestFollowPinsToBottomAndScrollUpReleases(t *testing.T) {
	m := newTestModel(t, 80, 5, defaultCols())
	m.follow = true
	for i := 0; i < 20; i++ {
		m.append([]string{`{"eventTimestamp":"2026-10-01T09:58:01.000Z","message":"line"}`})
	}
	if m.cursor != 19 {
		t.Errorf("follow mode left the cursor at %d, want the last record", m.cursor)
	}

	m.moveCursor(-5)
	if m.follow {
		t.Error("scrolling up must leave follow mode")
	}
	top := m.top
	m.append([]string{`{"eventTimestamp":"2026-10-01T09:58:02.000Z","message":"more"}`})
	if m.top != top {
		t.Errorf("view moved from %d to %d while not following", top, m.top)
	}
}

func TestEnterOpensPopupWithIndentedEmbeddedJSON(t *testing.T) {
	m := newTestModel(t, 100, 24, defaultCols(),
		`{"eventTimestamp":"2026-10-01T09:58:01.000Z","message":"failed","detail":"{\"status\":502}"}`)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.mode != modePopup {
		t.Fatal("enter did not open the popup")
	}
	body := strings.Join(m.popup, "\n")
	if !strings.Contains(body, `"status": 502`) {
		t.Errorf("popup body did not indent the embedded JSON:\n%s", body)
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, "502") {
		t.Errorf("popup not drawn over the list:\n%s", view)
	}
	if got := len(strings.Split(m.View(), "\n")); got != 24 {
		t.Errorf("popup overlay changed the view height to %d, want 24", got)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).mode != modeList {
		t.Error("esc did not close the popup")
	}
}

func TestEnterOnEmptyViewDoesNotPanic(t *testing.T) {
	m := newTestModel(t, 80, 10, defaultCols())
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.(Model).mode != modeList {
		t.Error("popup opened with no records")
	}
	_ = updated.(Model).View()
}

func TestSearchModeTyping(t *testing.T) {
	m := newTestModel(t, 80, 10, defaultCols(),
		`{"eventTimestamp":"2026-10-01T09:58:01.000Z","message":"alpha beta"}`)
	var mm tea.Model = m
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range "be ta" {
		if r == ' ' {
			mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeySpace})
			continue
		}
		mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := mm.(Model)
	if got.search != "be" {
		t.Errorf("search = %q, want %q", got.search, "be")
	}
	if len(got.matches) != 1 {
		t.Errorf("matches = %v, want 1", got.matches)
	}
}

func TestQuitKey(t *testing.T) {
	m := newTestModel(t, 80, 10, defaultCols())
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}); cmd == nil {
		t.Error("q did not return a command, expected tea.Quit")
	}
}

func TestTinyTerminalDoesNotPanic(t *testing.T) {
	m := newTestModel(t, 1, 1, defaultCols(),
		`{"eventTimestamp":"2026-10-01T09:58:01.000Z","message":"x"}`)
	_ = m.View()
	m.mode = modeHelp
	m.popup = helpLines()
	_ = m.View()
}

// stripANSI removes escape sequences so tests can assert on plain text.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func TestColouredRowsStillRespectTerminalWidth(t *testing.T) {
	// Styling is applied after the horizontal slice; if that order ever flips,
	// escape sequences leak into the width arithmetic and rows overflow.
	ch := make(chan string)
	m := New(Config{
		Columns: []string{record.DefaultTsField, "level", record.MessageField},
		TsField: record.DefaultTsField,
		Title:   "test",
		Color:   true,
	}, ch)
	m.w, m.h = 40, 8
	m.append([]string{
		`{"eventTimestamp":"2026-10-01T09:58:01.000Z","level":"error","message":"` + strings.Repeat("e", 200) + `"}`,
		`a broken line that is also quite long indeed, well over forty columns wide`,
	})
	m.setSearch("e")
	for i, line := range strings.Split(m.View(), "\n") {
		if n := len([]rune(stripANSI(line))); n > 40 {
			t.Errorf("coloured line %d is %d cells wide, want <= 40", i, n)
		}
	}
}

func TestMultiRuneKeyMessageIsReplayedPerRune(t *testing.T) {
	// Holding j, or a fast repeat, delivers all the runes in one KeyMsg.
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = `{"eventTimestamp":"2026-10-01T09:58:01.000Z","message":"line"}`
	}
	m := newTestModel(t, 80, 10, defaultCols(), lines...)
	m.cursor, m.top, m.follow = 0, 0, false

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jjjjj")})
	if got := updated.(Model).cursor; got != 5 {
		t.Errorf("cursor = %d after one jjjjj message, want 5", got)
	}

	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("kk")})
	got := updated.(Model)
	if got.cursor != 3 {
		t.Errorf("cursor = %d after kk, want 3", got.cursor)
	}
	if got.follow {
		t.Error("a burst of k must leave follow mode")
	}
}

func TestMultiRuneBurstStillQuits(t *testing.T) {
	m := newTestModel(t, 80, 10, defaultCols(),
		`{"eventTimestamp":"2026-10-01T09:58:01.000Z","message":"x"}`)
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jjq")}); cmd == nil {
		t.Error("q inside a rune burst did not quit")
	}
}

func TestSlashMidBurstStartsSearchWithTheRest(t *testing.T) {
	m := newTestModel(t, 80, 10, defaultCols(),
		`{"eventTimestamp":"2026-10-01T09:58:01.000Z","message":"alpha"}`,
		`{"eventTimestamp":"2026-10-01T09:58:02.000Z","message":"beta"}`)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/bet")})
	got := updated.(Model)
	if got.mode != modeSearch {
		t.Fatalf("mode = %v, want search", got.mode)
	}
	if got.input != "bet" {
		t.Errorf("input = %q, want %q", got.input, "bet")
	}
}

func TestHeaderAlignsWithRows(t *testing.T) {
	m := newTestModel(t, 100, 8, []string{record.DefaultTsField, "level", "service", record.MessageField},
		`{"eventTimestamp":"2026-10-01T09:58:01.000Z","level":"info","service":"api","message":"up"}`)
	lines := strings.Split(stripANSI(m.View()), "\n")
	header, row := lines[0], lines[1]
	if i, j := strings.Index(header, "LEVEL"), strings.Index(row, "info"); i != j {
		t.Errorf("LEVEL at column %d but its value at %d\n%s\n%s", i, j, header, row)
	}
	if i, j := strings.Index(header, "SERVICE"), strings.Index(row, "api"); i != j {
		t.Errorf("SERVICE at column %d but its value at %d\n%s\n%s", i, j, header, row)
	}
	if i, j := strings.Index(header, "MESSAGE"), strings.Index(row, "up"); i != j {
		t.Errorf("MESSAGE at column %d but its value at %d\n%s\n%s", i, j, header, row)
	}
}

func TestClippingDoesNotChangeWhatIsDisplayed(t *testing.T) {
	// The row builder clips to the visible window; the result must be identical
	// to what an unclipped build would have shown at every offset.
	msg := ""
	for i := 0; i < 400; i++ {
		msg += string(rune('a' + i%26))
	}
	m := newTestModel(t, 30, 5, defaultCols(),
		`{"eventTimestamp":"2026-10-01T09:58:01.000Z","message":"`+msg+`"}`)
	full := "05:58:01.000  " + msg // what the row would be, unclipped
	for _, off := range []int{0, 1, 7, 13, 14, 50, 200, 399, 413} {
		m.xoff = off
		got := strings.TrimRight(stripANSI(rows(m)[0]), " ")
		want := strings.TrimRight(clipWindow(full, off, 30), " ")
		if got != want {
			t.Errorf("xoff %d:\n got %q\nwant %q", off, got, want)
		}
	}
}

// clipWindow is the obvious, slow implementation the renderer must agree with.
func clipWindow(s string, off, w int) string {
	r := []rune(s)
	if off >= len(r) {
		return ""
	}
	end := off + w
	if end > len(r) {
		end = len(r)
	}
	return string(r[off:end])
}

func TestHugeRecordStaysResponsive(t *testing.T) {
	huge := strings.Repeat("z", 1<<20)
	m := newTestModel(t, 80, 10, defaultCols(),
		`{"eventTimestamp":"2026-10-01T09:58:01.000Z","message":"`+huge+`"}`)
	if got := len(stripANSI(strings.Split(m.View(), "\n")[0])); got > 80 {
		t.Errorf("first row is %d cells wide, want <= 80", got)
	}
	// $ must still reach the real end of the record.
	m.xoff = m.maxXOff()
	row := stripANSI(rows(m)[0])
	if !strings.Contains(row, "z") {
		t.Errorf("$ landed past the end of the record: %q", row)
	}
	if want := 14 + (1 << 20) - hScrollStep; m.maxXOff() != want {
		t.Errorf("maxXOff = %d, want %d", m.maxXOff(), want)
	}
}

func TestStatusHintsWhenThePopupWouldRevealJSON(t *testing.T) {
	m := newTestModel(t, 100, 8, defaultCols(),
		`{"eventTimestamp":"2026-10-01T09:58:01.000Z","message":"plain text"}`,
		`{"eventTimestamp":"2026-10-01T09:58:02.000Z","message":"failed: {\"status\":502}"}`)
	m.cursor = 0
	if strings.Contains(stripANSI(m.View()), "json") {
		t.Error("status hints at embedded JSON for a plain record")
	}
	m.cursor = 1
	if !strings.Contains(stripANSI(m.View()), "json") {
		t.Error("status does not hint at the embedded JSON")
	}
}
