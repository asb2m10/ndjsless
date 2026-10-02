package ui

import (
	"fmt"
	"strings"

	"github.com/asb2m10/ndjsless/internal/record"
	"github.com/charmbracelet/x/ansi"
)

// popupBox returns the inner content size of the popup: the border takes two
// cells each way and Padding(0,1) one more on each side. The popup fills the
// whole terminal rather than floating over the list.
func (m Model) popupBox() (w, h int) {
	w = max(20, m.w) - 4
	h = max(3, m.h) - 3 // two border rows plus the footer
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

// styleCopyMarker paints the record.CopyMarker line as a red button -- the
// visual cue that "m" copies the message to the clipboard. Done once at popup
// build time rather than in wrappedPopup, since wrappedPopup re-slices these
// lines on every render.
func styleCopyMarker(lines []string, st styles) []string {
	for i, l := range lines {
		if l == record.CopyMarker {
			lines[i] = st.copyBtn.Render(l)
		}
	}
	return lines
}

func (m Model) clampPopupTop(v int) int {
	h := m.popupHeight()
	maxTop := max(0, len(m.wrappedPopup())-h)
	return clamp(v, 0, maxTop)
}

// overlayPopup centres the popup on top of the already-rendered list.
func (m Model) overlayPopup(background string) string {
	w, h := m.popupBox()
	lines := m.wrappedPopup()

	body := make([]string, 0, h+1)
	for i := 0; i < h; i++ {
		idx := m.popupTop + i
		if idx < len(lines) {
			body = append(body, pad(ansi.Truncate(lines[idx], w, "›"), w))
		} else {
			body = append(body, strings.Repeat(" ", w))
		}
	}
	body = append(body, m.st.ts.Render(pad(m.popupFooter(len(lines), h), w)))

	box := m.st.popup.Render(strings.Join(body, "\n"))
	return compose(background, box, m.w, m.h)
}

func (m Model) popupFooter(total, h int) string {
	title := "record"
	if m.mode == modeHelp {
		title = "help"
	}
	base := fmt.Sprintf("%s · %d lines · q to close", title, total)
	if total > h {
		base = fmt.Sprintf("%s · %d-%d of %d · j/k to scroll · q to close",
			title, m.popupTop+1, min(m.popupTop+h, total), total)
	}
	if m.status != "" {
		return m.status + " — " + base
	}
	return base
}

// compose draws box centred over background, replacing the rows it covers rather
// than blending, which keeps the popup opaque without a cell buffer.
func compose(background, box string, w, h int) string {
	bg := strings.Split(background, "\n")
	fg := strings.Split(box, "\n")

	boxW := 0
	for _, l := range fg {
		if n := ansi.StringWidth(l); n > boxW {
			boxW = n
		}
	}
	left := max(0, (w-boxW)/2)
	top := max(0, (h-len(fg))/2)

	for i, l := range fg {
		row := top + i
		if row < 0 || row >= len(bg) {
			continue
		}
		before := ansi.Cut(bg[row], 0, left)
		before = pad(before, left)
		after := ansi.Cut(bg[row], left+boxW, w)
		bg[row] = before + l + after
	}
	return strings.Join(bg, "\n")
}
