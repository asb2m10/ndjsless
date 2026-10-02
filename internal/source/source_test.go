package source

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLinesReadsToEOF(t *testing.T) {
	ch := Lines(context.Background(), strings.NewReader("a\nb\nc\n"), false)
	var got []string
	for l := range ch {
		got = append(got, l)
	}
	if strings.Join(got, ",") != "a,b,c" {
		t.Errorf("got %v, want [a b c]", got)
	}
}

func TestLinesClosesChannelWithoutFollow(t *testing.T) {
	ch := Lines(context.Background(), strings.NewReader("a\n"), false)
	<-ch
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("unexpected extra line")
		}
	case <-time.After(time.Second):
		t.Error("channel not closed at EOF when not following")
	}
}

func TestLinesHandlesVeryLongLine(t *testing.T) {
	long := strings.Repeat("x", 1<<20) // 1 MiB, far past bufio's default
	ch := Lines(context.Background(), strings.NewReader(long+"\n"), false)
	got, ok := <-ch
	if !ok || len(got) != len(long) {
		t.Errorf("got %d bytes, want %d", len(got), len(long))
	}
}

func TestFollowPicksUpAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(path, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := Lines(ctx, f, true)

	if got := <-ch; got != "first" {
		t.Fatalf("got %q, want first", got)
	}

	w, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString("second\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()

	select {
	case got := <-ch:
		if got != "second" {
			t.Errorf("got %q, want second", got)
		}
	case <-time.After(5 * time.Second):
		t.Error("follow mode did not pick up the appended line")
	}
}

func TestCancelStopsTheReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(path)
	defer f.Close()

	ctx, cancel := context.WithCancel(context.Background())
	ch := Lines(ctx, f, true)
	<-ch
	cancel()
	select {
	case <-ch: // drained and closed
	case <-time.After(5 * time.Second):
		t.Error("cancelling the context did not stop the follower")
	}
}

func TestEmptyInput(t *testing.T) {
	ch := Lines(context.Background(), strings.NewReader(""), false)
	if _, ok := <-ch; ok {
		t.Error("empty input produced a line")
	}
}
