package rapidyenc

import (
	"fmt"
	"io"
)

const (
	defaultReadBufSize = 32 * 1024
	maxReadBufSize     = 8 * 1024 * 1024
)

type readBuffer struct {
	buf        []byte
	start, end int
}

func (rb *readBuffer) init() {
	if len(rb.buf) == 0 {
		rb.buf = make([]byte, defaultReadBufSize)
	}
}

func (rb *readBuffer) window() []byte {
	return rb.buf[rb.start:rb.end]
}

func (rb *readBuffer) advance(consumed int) {
	if consumed <= 0 {
		return
	}
	rb.start += consumed
	if rb.start >= rb.end {
		rb.start, rb.end = 0, 0
	}
}

func (rb *readBuffer) compact() {
	if rb.start == 0 || rb.start == rb.end {
		return
	}
	copy(rb.buf, rb.buf[rb.start:rb.end])
	rb.end -= rb.start
	rb.start = 0
}

func (rb *readBuffer) ensureWriteSpace() error {
	if rb.end < len(rb.buf) {
		return nil
	}
	if rb.start > 0 {
		rb.compact()
		if rb.end < len(rb.buf) {
			return nil
		}
	}

	// No space and cannot compact: grow.
	cur := len(rb.buf)
	if cur == 0 {
		cur = defaultReadBufSize
	}
	newLen := cur * 2
	if newLen > maxReadBufSize {
		newLen = maxReadBufSize
	}
	if newLen <= len(rb.buf) {
		return fmt.Errorf("nntp read buffer exceeded %d bytes", maxReadBufSize)
	}

	nb := make([]byte, newLen)
	copy(nb, rb.window())
	rb.end = rb.end - rb.start
	rb.start = 0
	rb.buf = nb
	return nil
}

func (rb *readBuffer) readMore(r io.Reader) (int, error) {
	if err := rb.ensureWriteSpace(); err != nil {
		return 0, err
	}
	n, err := r.Read(rb.buf[rb.end:])
	if n > 0 {
		rb.end += n
	}
	return n, err
}

func (rb *readBuffer) feedUntilDone(r io.Reader, feeder streamFeeder) error {
	rb.init()

	for {
		// Ensure we have some bytes to feed.
		if rb.start == rb.end {
			rb.start, rb.end = 0, 0
			if _, err := rb.readMore(r); err != nil {
				return err
			}
		}

		consumed, done, err := feeder.feed(rb.window())
		if consumed > 0 {
			rb.advance(consumed)
		}
		if err != nil {
			return err
		}
		if done {
			return nil
		}

		// Progress was made, so decode what is left before waiting on the reader.
		if consumed > 0 && rb.start < rb.end {
			continue
		}

		// Need more data.
		// If decoder couldn't consume anything but we have buffered bytes,
		// compact them to the start so the next read appends contiguously.
		if consumed == 0 && (rb.end-rb.start) > 0 {
			rb.compact()
		}

		if _, err := rb.readMore(r); err != nil {
			return err
		}
	}
}
