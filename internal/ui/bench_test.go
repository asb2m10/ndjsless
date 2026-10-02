package ui

import (
	"strings"
	"testing"

	"github.com/asb2m10/ndjsless/internal/record"
)

func benchModel(b *testing.B, msgLen, rows int) Model {
	b.Helper()
	ch := make(chan string)
	m := New(Config{
		Columns: []string{record.DefaultTsField, record.MessageField},
		TsField: record.DefaultTsField,
		Color:   true,
	}, ch)
	m.w, m.h = 120, 40
	lines := make([]string, rows)
	for i := range lines {
		lines[i] = `{"eventTime":"2026-10-01T09:58:01.000Z","level":"info","message":"` +
			strings.Repeat("z", msgLen) + `"}`
	}
	m.append(lines)
	return m
}

func BenchmarkViewHugeLines(b *testing.B) {
	m := benchModel(b, 1<<20, 40)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkViewNormalLines(b *testing.B) {
	m := benchModel(b, 120, 40)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkMaxXOffHugeLines(b *testing.B) {
	m := benchModel(b, 1<<20, 40)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.maxXOff()
	}
}
