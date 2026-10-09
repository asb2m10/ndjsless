package ui

import (
	"fmt"
	"strings"

	"github.com/asb2m10/ndjsless/internal/record"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// colGap separates columns.
const colGap = "  "

func (m Model) View() string {
	if m.mode == modePopup || m.mode == modeHelp || m.mode == modeMenu {
		return m.popupView()
	}
	var b strings.Builder

	if m.showHeader() {
		b.WriteString(m.st.header.Render(m.slice(m.headerText())))
		b.WriteByte('\n')
	}

	h := m.viewHeight()
	for i := 0; i < h; i++ {
		pos := m.top + i
		if pos >= len(m.rows) {
			b.WriteByte('\n')
			continue
		}
		b.WriteString(m.renderRow(pos))
		b.WriteByte('\n')
	}
	b.WriteString(m.statusLine())

	return b.String()
}

// headerText labels the columns. Labels are clipped to the column width, or the
// header would drift out of alignment with the rows beneath it.
func (m Model) headerText() string {
	cells := make([]string, 0, len(m.cols))
	for _, c := range m.cols {
		label := strings.ToUpper(lastSegment(c.name))
		if c.isTs {
			label = "TIME" // eventTime does not fit in HH:MM:SS.mmm
		}
		cells = append(cells, pad(truncate(label, c.width), c.width))
	}
	return strings.Join(cells, colGap)
}

// renderRow lays out the record at row position pos, slices it to the horizontal
// window, then styles it. Styling comes last on purpose: escape sequences would
// otherwise corrupt the cell arithmetic of the slice.
func (m Model) renderRow(pos int) string {
	r := m.recs[m.rows[pos]]
	visible := m.slice(m.rowText(r))

	base := m.st.forLevel(r.Level)
	if r.Broken {
		base = m.st.broken
	}
	if pos == m.cursor {
		base = base.Inherit(m.st.cursor)
		// Fill the rest of the line so the selection reads as a full-width bar.
		visible = pad(visible, m.w)
	}
	return highlight(visible, m.search, base, m.st.match)
}

// rowText builds the unstyled text of a record row, clipped to the horizontal
// window plus a small margin.
//
// The clip is what keeps the pager responsive: a single log record can carry a
// megabyte of payload, and building or measuring all of it for every frame costs
// more than redrawing the entire screen. Only the cells that can actually appear
// are ever touched.
func (m Model) rowText(r record.Record) string {
	budget := m.xoff + m.w + 8
	cells := make([]string, 0, len(m.cols))
	for i, c := range m.cols {
		var v string
		if c.isTs {
			v = r.Timestamp()
		} else {
			v = r.Column(c.name)
		}
		if i == len(m.cols)-1 && c.width == 0 {
			// The last column is unbounded, so clip it before any O(n) work.
			cells = append(cells, sanitise(clipRunes(v, budget)))
			continue
		}
		cells = append(cells, pad(truncate(sanitise(clipRunes(v, c.width*2+1)), c.width), c.width))
	}
	return strings.TrimRight(strings.Join(cells, colGap), " ")
}

// clipRunes returns the first n runes of s. A rune occupies at least one cell, so
// n runes always cover at least n cells, which is all the caller needs.
func clipRunes(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s // bytes >= runes, so this is already short enough
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// tailWidth is the display width of a record's last column, which is the only
// part of a row whose length is unbounded. It is measured once per record so
// that maxXOff does not have to rescan megabyte payloads on every keypress.
func (m Model) tailWidth(r record.Record) int {
	last := m.cols[len(m.cols)-1]
	var v string
	if last.isTs {
		v = r.Timestamp()
	} else {
		v = r.Column(last.name)
	}
	return ansi.StringWidth(sanitise(v))
}

// prefixWidth is the width of every column before the last one, gaps included.
func (m Model) prefixWidth() int {
	w := 0
	for i, c := range m.cols {
		if i == len(m.cols)-1 {
			break
		}
		w += c.width + len(colGap)
	}
	return w
}

// slice applies the horizontal scroll offset and the terminal width.
func (m Model) slice(s string) string {
	return ansi.Cut(s, m.xoff, m.xoff+m.w)
}

// highlight paints s with base, re-painting case-insensitive occurrences of
// needle with the match style.
func highlight(s, needle string, base, match lipgloss.Style) string {
	if needle == "" || s == "" {
		return base.Render(s)
	}
	lowS, lowN := strings.ToLower(s), strings.ToLower(needle)
	var b strings.Builder
	for {
		i := strings.Index(lowS, lowN)
		if i < 0 {
			b.WriteString(base.Render(s))
			return b.String()
		}
		b.WriteString(base.Render(s[:i]))
		b.WriteString(match.Render(s[i : i+len(needle)]))
		s, lowS = s[i+len(needle):], lowS[i+len(needle):]
	}
}

// statusLine is the bottom bar: source, count, follow state and search position.
func (m Model) statusLine() string {
	left := m.cfg.Title
	if m.mode == modeSearch {
		return m.st.status.Render(pad("/"+m.input, m.w))
	}
	if m.mode == modeFilterValue {
		return m.st.status.Render(pad("filter "+m.filterOn+"="+m.input, m.w))
	}

	parts := []string{left, fmt.Sprintf("%d lines", len(m.recs))}
	switch {
	case m.filtering && m.filter == nil:
		parts = []string{left, fmt.Sprintf("clearing filter over %d lines…", len(m.recs))}
	case m.filtering:
		parts = []string{left, fmt.Sprintf("filtering %d lines…", len(m.recs)),
			fmt.Sprintf("%s=%s", m.filter.field, truncate(m.filter.value, 40))}
	case m.filter != nil:
		parts = []string{left, fmt.Sprintf("%d of %d lines", len(m.rows), len(m.recs)),
			fmt.Sprintf("%s=%s", m.filter.field, truncate(m.filter.value, 40))}
	}
	if m.follow {
		parts = append(parts, "FOLLOW")
	} else if m.closed {
		parts = append(parts, "END")
	}
	if m.search != "" {
		pos := "-"
		for i, idx := range m.matches {
			if idx == m.cursor {
				pos = fmt.Sprint(i + 1)
				break
			}
		}
		parts = append(parts, fmt.Sprintf("/%s (%s/%d)", m.search, pos, len(m.matches)))
	}
	if m.xoff > 0 {
		parts = append(parts, fmt.Sprintf("+%d", m.xoff))
	}
	if m.cursor < len(m.rows) && m.hasJSON[m.rows[m.cursor]] {
		parts = append(parts, "⏎ json")
	}
	bar := strings.Join(parts, " · ")
	if m.status != "" {
		bar = m.status + " — " + bar
	}
	bar += "  (? for help)"

	// Keep the right-hand information when the terminal is narrow.
	if ansi.StringWidth(bar) > m.w {
		bar = ansi.TruncateLeft(bar, ansi.StringWidth(bar)-m.w+1, "<")
	}
	return m.st.status.Render(pad(bar, m.w))
}

// pad right-pads to width cells, counting display cells rather than bytes.
func pad(s string, width int) string {
	if width <= 0 {
		return s
	}
	n := ansi.StringWidth(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

// truncate clips to width cells with an ellipsis, leaving width 0 untouched.
func truncate(s string, width int) string {
	if width <= 0 || ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// sanitise flattens anything that would break the one-record-per-row invariant:
// embedded newlines, tabs and carriage returns.
func sanitise(s string) string {
	if !strings.ContainsAny(s, "\n\r\t") {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.ReplaceAll(s, "\t", "    ")
}

// lastSegment turns a dotted path into a short header label.
func lastSegment(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}
