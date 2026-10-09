package ui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// styles holds every attribute the UI paints with. When colour is disabled every
// field is the identity style, so the render path needs no conditionals.
type styles struct {
	enabled bool

	header   lipgloss.Style
	status   lipgloss.Style
	statusHi lipgloss.Style
	ts       lipgloss.Style
	cursor   lipgloss.Style
	match    lipgloss.Style
	broken   lipgloss.Style
	levels   map[string]lipgloss.Style
	level    lipgloss.Style
}

// ColourEnabled honours NO_COLOR (https://no-color.org) and TERM=dumb.
func ColourEnabled() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return os.Getenv("TERM") != "dumb"
}

func newStyles(enabled bool) styles {
	if !enabled {
		plain := lipgloss.NewStyle()
		return styles{
			header: plain, status: plain.Reverse(true), statusHi: plain.Reverse(true),
			ts: plain, cursor: plain.Reverse(true), match: plain.Underline(true),
			broken: plain, level: plain,
			levels: map[string]lipgloss.Style{},
		}
	}
	return styles{
		enabled:  true,
		header:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")),
		status:   lipgloss.NewStyle().Foreground(lipgloss.Color("236")).Background(lipgloss.Color("248")),
		statusHi: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("22")).Background(lipgloss.Color("248")),
		ts:       lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		cursor:   lipgloss.NewStyle().Background(lipgloss.Color("238")),
		match:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("16")).Background(lipgloss.Color("221")),
		broken:   lipgloss.NewStyle().Faint(true).Italic(true),
		level:    lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		levels: map[string]lipgloss.Style{
			"fatal": lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("201")),
			"error": lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
			"warn":  lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
			"info":  lipgloss.NewStyle(),
			"debug": lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
			"trace": lipgloss.NewStyle().Faint(true),
		},
	}
}

// forLevel returns the row style for a severity, falling back to plain text for
// levels we do not recognise.
func (s styles) forLevel(level string) lipgloss.Style {
	if st, ok := s.levels[level]; ok {
		return st
	}
	return lipgloss.NewStyle()
}
