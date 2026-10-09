package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// popupBox returns the content size of the popup. It is called a popup but it
// replaces the whole screen, with no border or padding: anything drawn around
// the text would be picked up by the terminal's own mouse selection.
func (m Model) popupBox() (w, h int) {
	w = max(1, m.w)
	h = max(1, m.h-1) // the footer takes the last row
	return w, h
}

func (m Model) popupHeight() int {
	_, h := m.popupBox()
	return h
}

// wrappedPopup word-wraps the popup body to the box width. This is the only
// place in the program where text wraps; the record list never does.
func (m Model) wrappedPopup() []string {
	w, _ := m.popupBox()
	var out []string
	for _, line := range m.popup {
		line = strings.ReplaceAll(line, "\t", "    ")
		if ansi.StringWidth(line) <= w {
			out = append(out, line)
			continue
		}
		// Preserve the JSON indent on continuation lines so nesting stays readable.
		indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
		if ansi.StringWidth(indent)+8 > w {
			indent = ""
		}
		wrapped := strings.Split(ansi.Wrap(line, w, ""), "\n")
		for i, wl := range wrapped {
			if i == 0 {
				out = append(out, wl)
			} else {
				out = append(out, ansi.Truncate(indent+wl, w, ""))
			}
		}
	}
	return out
}

func (m Model) clampPopupTop(v int) int {
	h := m.popupHeight()
	maxTop := max(0, len(m.wrappedPopup())-h)
	return clamp(v, 0, maxTop)
}

// popupView renders the popup full screen in place of the list. Lines are not
// padded to the width, so selecting text does not drag trailing blanks along.
func (m Model) popupView() string {
	w, h := m.popupBox()
	lines := m.wrappedPopup()
	if m.mode == modeMenu {
		lines = m.menuLines()
	}

	body := make([]string, 0, h+1)
	for i := 0; i < h; i++ {
		idx := m.popupTop + i
		if idx < len(lines) {
			body = append(body, ansi.Truncate(lines[idx], w, "›"))
		} else {
			body = append(body, "")
		}
	}
	body = append(body, m.st.ts.Render(ansi.Truncate(m.popupFooter(len(lines), h), w, "")))
	return strings.Join(body, "\n")
}

// menuLines renders the ! menu, with the cursor row highlighted.
func (m Model) menuLines() []string {
	labels := append([]string{"reset filter"}, m.menu...)
	out := make([]string, len(labels))
	for i, label := range labels {
		if i == m.menuCur {
			out[i] = m.st.cursor.Render("> " + label)
		} else {
			out[i] = "  " + label
		}
	}
	return out
}

func (m Model) popupFooter(total, h int) string {
	title := "record"
	switch m.mode {
	case modeHelp:
		title = "help"
	case modeMenu:
		title = "filter by field"
	}
	base := fmt.Sprintf("%s · %d lines · q to close", title, total)
	if m.mode == modeMenu {
		base = fmt.Sprintf("%s · j/k · enter to choose · q to close", title)
	}
	if total > h {
		base = fmt.Sprintf("%s · %d-%d of %d · j/k to scroll · q to close",
			title, m.popupTop+1, min(m.popupTop+h, total), total)
	}
	if m.status != "" {
		return m.status + " — " + base
	}
	return base
}
