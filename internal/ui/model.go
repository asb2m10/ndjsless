// Package ui implements the pager: a less-like viewport over ndjson records with
// horizontal scrolling, search, follow mode and a detail popup.
package ui

import (
	"io"
	"strings"

	"github.com/asb2m10/ndjsless/internal/record"
	tea "github.com/charmbracelet/bubbletea"
)

// Config is everything the pager needs from the command line.
type Config struct {
	Columns   []string // column names in render order; one of them may be TsField
	TsField   string   // which column is the timestamp
	Title     string   // source name, shown in the status bar
	Follow    bool     // start pinned to the bottom
	Color     bool
	Clipboard io.Writer // where OSC52 copy sequences are written; nil disables c/m
}

type mode int

const (
	modeList mode = iota
	modeSearch
	modePopup
	modeHelp
)

// hScrollStep is how far h/l move, in cells.
const hScrollStep = 8

// tsWidth is the fixed width of the timestamp column: HH:MM:SS.mmm.
const tsWidth = 12

// colClamp bounds how wide a non-message column may grow.
const colClamp = 24

// maxBatch is how many pending lines we fold into one update.
const maxBatch = 500

type column struct {
	name  string
	width int // 0 means "unbounded", used for the last column
	isTs  bool
}

// Model is the bubbletea model.
type Model struct {
	cfg   Config
	st    styles
	cols  []column
	lines <-chan string

	recs    []record.Record
	tailW   []int  // display width of each record's last column; see tailWidth
	hasJSON []bool // whether the popup would reveal indented JSON

	top    int // index of the first visible record
	cursor int // selected record
	xoff   int // horizontal scroll, in cells
	follow bool

	search  string
	matches []int // record indices matching search, ascending
	status  string

	mode     mode
	input    string // search being typed
	popup    []string
	popupTop int

	w, h   int
	closed bool
}

// New builds a model over a stream of raw lines.
func New(cfg Config, lines <-chan string) Model {
	cols := make([]column, 0, len(cfg.Columns))
	for i, name := range cfg.Columns {
		c := column{name: name, isTs: name == cfg.TsField}
		switch {
		case c.isTs:
			c.width = tsWidth
		case i == len(cfg.Columns)-1:
			c.width = 0 // last column runs to the end of the line
		default:
			c.width = len(name)
		}
		cols = append(cols, c)
	}
	return Model{
		cfg:    cfg,
		st:     newStyles(cfg.Color),
		cols:   cols,
		lines:  lines,
		follow: cfg.Follow,
		w:      80,
		h:      24,
	}
}

// linesMsg carries a batch of raw lines read from the source.
type linesMsg []string

// closedMsg says the source is exhausted.
type closedMsg struct{}

func (m Model) Init() tea.Cmd {
	return waitForLines(m.lines)
}

// waitForLines blocks for one line, then folds whatever else is already buffered
// into the same message. Without this, a busy log would trigger one full
// re-render per line and the UI would fall behind the writer.
func waitForLines(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return closedMsg{}
		}
		batch := []string{line}
		for len(batch) < maxBatch {
			select {
			case l, ok := <-ch:
				if !ok {
					return linesMsg(batch)
				}
				batch = append(batch, l)
			default:
				return linesMsg(batch)
			}
		}
		return linesMsg(batch)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.clampVertical()
		return m, nil

	case linesMsg:
		m.append(msg)
		return m, waitForLines(m.lines)

	case closedMsg:
		m.closed = true
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// append ingests new lines, keeping the view pinned to the bottom in follow mode.
func (m *Model) append(lines []string) {
	for _, line := range lines {
		r := record.Parse(line, m.cfg.TsField)
		m.recs = append(m.recs, r)
		m.tailW = append(m.tailW, m.tailWidth(r))
		// Computed on ingest, not on render: finding embedded JSON means scanning
		// the record, and the status bar must not pay that on every frame.
		m.hasJSON = append(m.hasJSON, record.HasEmbedded(r))
		m.growColumns(r)
		if m.search != "" && matchesRecord(r, m.cols, m.cfg.TsField, m.search) {
			m.matches = append(m.matches, len(m.recs)-1)
		}
	}
	if m.follow {
		m.toBottom()
	}
	m.clampVertical()
}

// growColumns widens columns to fit new values. Widths only ever grow: shrinking
// them would make the whole view jitter sideways as records stream in.
func (m *Model) growColumns(r record.Record) {
	for i := range m.cols {
		c := &m.cols[i]
		if c.isTs || c.width == 0 {
			continue
		}
		if n := len(r.Column(c.name)); n > c.width {
			c.width = min(n, colClamp)
		}
	}
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeSearch {
		return m.handleSearchKey(msg)
	}
	// A key held down, or a fast repeat, arrives as one message carrying several
	// runes. Outside the search field each of those is its own command, so replay
	// them one at a time instead of looking up "jjj" and finding nothing. A `/`
	// mid-burst switches to search mode and the remaining runes become the
	// pattern, since handleKey re-dispatches on the mode each time.
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 {
		var model tea.Model = m
		for _, r := range msg.Runes {
			next, cmd := model.(Model).handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			model = next
			if cmd != nil {
				return model, cmd
			}
		}
		return model, nil
	}
	if m.mode == modePopup || m.mode == modeHelp {
		return m.handlePopupKey(msg)
	}

	m.status = ""
	page := m.viewHeight()
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "j", "down", "ctrl+n":
		m.moveCursor(1)
	case "k", "up", "ctrl+p":
		m.moveCursor(-1)
	case "d", "ctrl+d":
		m.moveCursor(page / 2)
	case "u", "ctrl+u":
		m.moveCursor(-page / 2)
	case "f", "ctrl+f", "pgdown", " ":
		m.moveCursor(page)
	case "b", "ctrl+b", "pgup":
		m.moveCursor(-page)

	case "g", "home":
		m.follow = false
		m.cursor, m.top = 0, 0
	case "G", "end":
		m.follow = true
		m.toBottom()

	case "h", "left":
		m.xoff = max(0, m.xoff-hScrollStep)
	case "l", "right":
		m.xoff = min(m.maxXOff(), m.xoff+hScrollStep)
	case "0":
		m.xoff = 0
	case "$":
		m.xoff = m.maxXOff()

	case "F":
		m.follow = !m.follow
		if m.follow {
			m.toBottom()
		}

	case "/":
		m.mode = modeSearch
		m.input = ""
	case "n":
		m.jumpMatch(1)
	case "N":
		m.jumpMatch(-1)

	case "enter":
		m.openPopup()
	case "?":
		m.mode = modeHelp
		m.popup = helpLines()
		m.popupTop = 0

	case "c":
		if m.cursor < len(m.recs) {
			copyToClipboard(m.cfg.Clipboard, m.recs[m.cursor].Raw)
			m.status = "copied line"
		}
	case "m":
		if m.cursor < len(m.recs) {
			copyToClipboard(m.cfg.Clipboard, m.recs[m.cursor].Column(record.MessageField))
			m.status = "copied message"
		}
	}
	return m, nil
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = modeList
		m.input = ""
	case "enter":
		m.mode = modeList
		m.setSearch(m.input)
		m.input = ""
	case "backspace":
		if m.input != "" {
			r := []rune(m.input)
			m.input = string(r[:len(r)-1])
		}
	default:
		if msg.Type == tea.KeyRunes {
			m.input += string(msg.Runes)
		} else if msg.Type == tea.KeySpace {
			m.input += " "
		}
	}
	return m, nil
}

func (m Model) handlePopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	page := m.popupHeight()
	m.status = ""
	switch msg.String() {
	case "q", "esc", "enter", "ctrl+c":
		m.mode = modeList
		m.popup = nil
		m.popupTop = 0
	case "m":
		if m.mode == modePopup && m.cursor < len(m.recs) {
			copyToClipboard(m.cfg.Clipboard, record.PrettyMessage(m.recs[m.cursor]))
			m.status = "copied message"
		}
	case "j", "down":
		m.popupTop = m.clampPopupTop(m.popupTop + 1)
	case "k", "up":
		m.popupTop = m.clampPopupTop(m.popupTop - 1)
	case "d", "ctrl+d", "pgdown", " ":
		m.popupTop = m.clampPopupTop(m.popupTop + page)
	case "u", "ctrl+u", "pgup", "b":
		m.popupTop = m.clampPopupTop(m.popupTop - page)
	case "g", "home":
		m.popupTop = 0
	case "G", "end":
		m.popupTop = m.clampPopupTop(len(m.wrappedPopup()))
	}
	return m, nil
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelDown:
		if m.mode == modePopup || m.mode == modeHelp {
			m.popupTop = m.clampPopupTop(m.popupTop + 3)
		} else {
			m.moveCursor(3)
		}
	case tea.MouseButtonWheelUp:
		if m.mode == modePopup || m.mode == modeHelp {
			m.popupTop = m.clampPopupTop(m.popupTop - 3)
		} else {
			m.moveCursor(-3)
		}
	case tea.MouseButtonWheelLeft:
		m.xoff = max(0, m.xoff-hScrollStep)
	case tea.MouseButtonWheelRight:
		m.xoff = min(m.maxXOff(), m.xoff+hScrollStep)
	}
	return m, nil
}

// moveCursor scrolls by delta lines. Moving up always leaves follow mode, which
// is what makes `tail -f | ndjsless` usable: you scroll back and the view holds
// still while the log keeps growing.
func (m *Model) moveCursor(delta int) {
	if delta == 0 || len(m.recs) == 0 {
		return
	}
	if delta < 0 {
		m.follow = false
	}
	m.cursor = clamp(m.cursor+delta, 0, len(m.recs)-1)
	m.scrollIntoView()
	if m.cursor == len(m.recs)-1 && delta > 0 {
		m.follow = true
	}
}

// scrollIntoView adjusts top so the cursor is visible.
func (m *Model) scrollIntoView() {
	h := m.viewHeight()
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+h {
		m.top = m.cursor - h + 1
	}
	m.clampVertical()
}

func (m *Model) toBottom() {
	if len(m.recs) == 0 {
		return
	}
	m.cursor = len(m.recs) - 1
	m.top = max(0, len(m.recs)-m.viewHeight())
}

func (m *Model) clampVertical() {
	maxTop := max(0, len(m.recs)-m.viewHeight())
	m.top = clamp(m.top, 0, maxTop)
	if len(m.recs) == 0 {
		m.cursor = 0
		return
	}
	m.cursor = clamp(m.cursor, 0, len(m.recs)-1)
}

// viewHeight is the number of record rows on screen: everything but the status
// line, and the header when columns are named.
func (m Model) viewHeight() int {
	h := m.h - 1
	if m.showHeader() {
		h--
	}
	return max(1, h)
}

// showHeader prints a column header row once there is more than the default
// timestamp+message pair to label.
func (m Model) showHeader() bool {
	return len(m.cols) > 2
}

// maxXOff bounds horizontal scrolling to the content actually on screen, so
// pressing `l` cannot strand the user in empty space. It reads the cached tail
// widths rather than re-rendering the rows, which matters when a record carries
// a megabyte of payload.
func (m Model) maxXOff() int {
	widest := 0
	for i := m.top; i < len(m.recs) && i < m.top+m.viewHeight(); i++ {
		if n := m.tailW[i]; n > widest {
			widest = n
		}
	}
	return max(0, m.prefixWidth()+widest-hScrollStep)
}

// setSearch installs a pattern and jumps to the first match at or after the
// cursor, like less does.
func (m *Model) setSearch(pattern string) {
	m.search = pattern
	m.matches = nil
	if pattern == "" {
		return
	}
	for i, r := range m.recs {
		if matchesRecord(r, m.cols, m.cfg.TsField, pattern) {
			m.matches = append(m.matches, i)
		}
	}
	if len(m.matches) == 0 {
		m.status = "Pattern not found: " + pattern
		return
	}
	for _, idx := range m.matches {
		if idx >= m.cursor {
			m.gotoRecord(idx)
			return
		}
	}
	m.gotoRecord(m.matches[0]) // wrapped around
}

// jumpMatch steps to the next (dir>0) or previous match, wrapping at the ends.
func (m *Model) jumpMatch(dir int) {
	if m.search == "" {
		m.status = "No previous search pattern"
		return
	}
	if len(m.matches) == 0 {
		m.status = "Pattern not found: " + m.search
		return
	}
	if dir > 0 {
		for _, idx := range m.matches {
			if idx > m.cursor {
				m.gotoRecord(idx)
				return
			}
		}
		m.gotoRecord(m.matches[0])
		m.status = "Search hit BOTTOM, continuing at top"
		return
	}
	for i := len(m.matches) - 1; i >= 0; i-- {
		if m.matches[i] < m.cursor {
			m.gotoRecord(m.matches[i])
			return
		}
	}
	m.gotoRecord(m.matches[len(m.matches)-1])
	m.status = "Search hit TOP, continuing at bottom"
}

// gotoRecord centres the view on an index and drops follow mode.
func (m *Model) gotoRecord(idx int) {
	m.follow = false
	m.cursor = clamp(idx, 0, max(0, len(m.recs)-1))
	m.scrollIntoView()
}

// openPopup renders the selected record in full, with embedded JSON indented.
func (m *Model) openPopup() {
	if len(m.recs) == 0 {
		return
	}
	m.mode = modePopup
	m.popup = strings.Split(record.Pretty(m.recs[m.cursor]), "\n")
	m.popupTop = 0
}

// matchesRecord reports a case-insensitive substring hit on any rendered column,
// so searching matches what the eye sees rather than the raw JSON.
func matchesRecord(r record.Record, cols []column, tsField, pattern string) bool {
	needle := strings.ToLower(pattern)
	for _, c := range cols {
		var v string
		if c.isTs {
			v = r.Timestamp()
		} else {
			v = r.Column(c.name)
		}
		if strings.Contains(strings.ToLower(v), needle) {
			return true
		}
	}
	return false
}

func helpLines() []string {
	return strings.Split(strings.TrimSpace(`
ndjsless — keys

  j / k, down / up      line down / up
  d / u                 half page down / up
  f / b, space, pgdn    full page down / up
  g / G, home / end     first / last line (G resumes follow)
  h / l, left / right   scroll left / right
  0 / $                 line start / furthest right
  /                     search
  n / N                 next / previous match
  enter                 show the full record, embedded JSON indented
  c                     copy the current line to the clipboard
  m                     copy the current message to the clipboard
  F                     toggle follow mode
  ?                     this help
  q                     quit

In the popup: j/k/d/u/g/G scroll, m copies the message, q or esc closes.
`), "\n")
}

func clamp(v, lo, hi int) int { return max(lo, min(hi, v)) }
