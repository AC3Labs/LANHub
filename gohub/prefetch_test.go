package main

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"testing/iotest"
)

func TestPrefetchReaderPassesEveryByteInOrder(t *testing.T) {
	src := make([]byte, 5<<20+123)
	for i := range src {
		src[i] = byte(i * 7)
	}
	p := newPrefetchReader(iotest.HalfReader(bytes.NewReader(src)), 64<<10, 4)
	got, err := io.ReadAll(p)
	if err != nil || !bytes.Equal(got, src) {
		t.Fatalf("got %d bytes, err %v", len(got), err)
	}
}

func TestPrefetchReaderReportsReadErrors(t *testing.T) {
	boom := errors.New("connection reset")
	p := newPrefetchReader(io.MultiReader(bytes.NewReader([]byte("partial")), iotest.ErrReader(boom)), 4, 2)
	got, err := io.ReadAll(p)
	if !errors.Is(err, boom) || string(got) != "partial" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestPrefetchReaderCloseStopsEarly(t *testing.T) {
	p := newPrefetchReader(bytes.NewReader(make([]byte, 1<<20)), 1024, 2)
	buf := make([]byte, 10)
	p.Read(buf)
	p.Close() // must not hang or panic
}
