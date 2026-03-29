package rapidyenc

import (
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"sync"

	"golang.org/x/sync/errgroup"
)

type Encoder struct {
	w        io.Writer
	m        Meta
	hWritten bool

	hash       hash.Hash32
	lineLength int
	raw        bool // Encode without yenc headers
	column     int
	processed  int64

	buf     []byte
	endByte []byte

	writeMu  sync.Mutex
	hashErrs errgroup.Group
}

type EncoderOption func(*Encoder)

// NewEncoder returns a new [Encoder].
// Writes to the returned writer are yEnc encoded and written to w.
//
// It is the caller's responsibility to call Close on the [Encoder] when done.
func NewEncoder(w io.Writer, m Meta, opts ...EncoderOption) (e *Encoder, err error) {
	e = &Encoder{
		lineLength: 128,
		hash:       crc32.NewIEEE(),
		endByte:    make([]byte, 0, 1),
	}

	for _, opt := range opts {
		opt(e)
	}

	if err := e.Reset(w, m); err != nil {
		return nil, err
	}

	return
}

// WithLineLength configures the encoded line length
func WithLineLength(lineLength int) EncoderOption {
	return func(e *Encoder) {
		e.lineLength = lineLength
	}
}

// WithRaw option encodes without writing yenc headers
func WithRaw() EncoderOption {
	return func(e *Encoder) {
		e.raw = true
	}
}

// Reset discards the [Encoder] e's state and makes it equivalent to the
// result of its original state from [NewEncoder], but writing to w instead.
// This permits reusing a [Encoder] rather than allocating a new one.
func (e *Encoder) Reset(w io.Writer, meta Meta) error {
	if !e.raw {
		if err := meta.validate(); err != nil {
			return err
		}
	}

	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	e.w = w
	e.m = meta
	e.hWritten = false
	e.hash.Reset()
	e.endByte = e.endByte[:0]
	e.processed = 0
	e.hashErrs = errgroup.Group{}

	return nil
}

var errWriterNil = errors.New("writer is nil")

// Write writes a yEnc encoded form of p to the underlying [io.Writer]. The
// encoded bytes are not necessarily flushed until the [Encoder] is closed.
func (e *Encoder) Write(p []byte) (n int, err error) {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	if e.w == nil {
		return 0, errWriterNil
	}

	n = len(p)

	e.hashErrs.Go(func() error {
		if _, e := e.hash.Write(p); e != nil {
			return e
		}
		return nil
	})
	defer func() {
		// Other errors take priority
		if hashErr := e.hashErrs.Wait(); err == nil {
			err = hashErr
		}
	}()

	if !e.raw {
		if _, err := e.writeHeader(); err != nil {
			return 0, err
		}
	}

	// Previous Write ended with a space or tab, so we need to include it (without escaping)
	if len(e.endByte) > 0 {
		if _, err := e.w.Write(e.endByte); err != nil {
			return 0, err
		}
		e.endByte = e.endByte[:0]
	}

	if len(p) > 0 {
		e.processed += int64(len(p))

		if grow := maxLength(len(p), e.lineLength) - len(e.buf); grow > 0 {
			e.buf = append(e.buf, make([]byte, grow)...)
		}

		buf := e.buf

		encoded := encodeIncremental(e.lineLength, &e.column, p, buf, false)

		length := len(encoded)

		if length > 0 {
			// If the last character is '\t' or ' ' then if this is the last write it will need escaping.
			// Therefore, save the byte for the next call to Write or Close.
			if encoded[length-1] == '\t' || encoded[length-1] == ' ' {
				e.endByte = append(e.endByte, encoded[length-1])
				buf = encoded[:length-1]
			} else {
				buf = encoded[:length]
			}

			if len(buf) > 0 {
				if _, err = e.w.Write(buf); err != nil {
					return 0, err
				}
			}
		}
	}

	return
}

// Close flushes any pending output from the encoder and writes the trailing header.
// It is an error to call Write after calling Close.
func (e *Encoder) Close() error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	if e.w == nil {
		return errWriterNil
	}
	defer func() { e.w = nil }()

	if len(e.endByte) > 0 {
		if _, err := e.w.Write([]byte{'=', e.endByte[0] + 64}); err != nil {
			return err
		}
	}

	if !e.raw {
		if _, err := fmt.Fprintf(e.w, "\r\n=yend size=%d part=%d pcrc32=%08x\r\n", e.m.PartSize, e.m.PartNumber, e.hash.Sum32()); err != nil {
			return err
		}

		if e.processed != e.m.PartSize {
			return fmt.Errorf(
				"[rapidyenc] encode header has part size %d but actually encoded %d bytes",
				e.m.PartSize, e.processed,
			)
		}
	}

	return nil
}

func (e *Encoder) writeHeader() (int, error) {
	if e.hWritten {
		return 0, nil
	}

	e.hWritten = true
	return fmt.Fprintf(
		e.w,
		"=ybegin part=%d total=%d line=%d size=%d name=%s\r\n=ypart begin=%d end=%d\r\n",
		e.m.PartNumber, e.m.TotalParts, e.lineLength, e.m.FileSize, e.m.FileName, e.m.Begin(), e.m.End(),
	)
}

// maxLength returns the maximum possible length of yEnc encoded output given the length of the unencoded data and line length.
// maxLength also includes additional padding needed by the rapidyenc implementation.
func maxLength(length, lineLength int) int {
	ret := length * 2 // all characters escaped
	ret += 2          // allocation for offset and that a newline may occur early
	ret += 64         // allocation for YMM overflowing

	// add newlines, considering the possibility of all chars escaped
	if lineLength == 128 {
		// optimize common case
		return ret + 2*(length>>6)
	}
	return ret + 2*((length*2)/lineLength)
}
