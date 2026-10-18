package main

import "io"

// prefetchReader reads ahead from r in its own goroutine, keeping up to
// depth chunks ready. A relay reads from one agent and writes to another;
// without this the two take turns (each 32 KB write waits for the next
// read), so neither link runs at full speed. Measured on nas: 7 MiB/s each
// way in turns versus the link's ~11.
type prefetchReader struct {
	ch   chan []byte
	cur  []byte
	err  error
	done chan struct{}
}

func newPrefetchReader(r io.Reader, chunk, depth int) *prefetchReader {
	p := &prefetchReader{ch: make(chan []byte, depth), done: make(chan struct{})}
	go func() {
		defer close(p.ch)
		for {
			buf := make([]byte, chunk)
			n, err := io.ReadFull(r, buf)
			if n > 0 {
				select {
				case p.ch <- buf[:n]:
				case <-p.done:
					return
				}
			}
			if err != nil {
				if err != io.ErrUnexpectedEOF && err != io.EOF {
					p.err = err
				}
				return
			}
		}
	}()
	return p
}

func (p *prefetchReader) Read(b []byte) (int, error) {
	for len(p.cur) == 0 {
		next, ok := <-p.ch
		if !ok {
			if p.err != nil {
				return 0, p.err
			}
			return 0, io.EOF
		}
		p.cur = next
	}
	n := copy(b, p.cur)
	p.cur = p.cur[n:]
	return n, nil
}

// Close stops the read-ahead goroutine if the consumer gives up early.
func (p *prefetchReader) Close() {
	select {
	case <-p.done:
	default:
		close(p.done)
	}
}
