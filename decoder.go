package rapidyenc

import (
	"errors"
	"io"
)

type Decoder struct {
	r                  io.Reader
	rb                 readBuffer
	statusLineConsumed bool // Has the caller already consumed the status line; if so trust that it is a multiline response
	dataFunc           func() []byte
}

type DecoderOption func(d *Decoder)

func NewDecoder(r io.Reader, opts ...DecoderOption) *Decoder {
	d := &Decoder{r: r}

	for _, opt := range opts {
		opt(d)
	}

	return d
}

// WithStatusLineAlreadyRead the decoder assumes the stream is positioned at the start of a multiline response body and
// the caller has already consumed the first line of the response
func WithStatusLineAlreadyRead() DecoderOption {
	return func(d *Decoder) {
		d.statusLineConsumed = true
	}
}

// WithBufferSize allows the caller to customise the size of the internal buffer used by the decoder
func WithBufferSize(size int) DecoderOption {
	return func(d *Decoder) {
		d.rb = readBuffer{buf: make([]byte, max(1024, size))}
	}
}

// WithDataFunc allows a function to be called when responses need a []byte, for example from a sync.Pool to
// reduce GC pressure
func WithDataFunc(dataFunc func() []byte) DecoderOption {
	return func(d *Decoder) {
		d.dataFunc = dataFunc
	}
}

var (
	ErrDataMissing    = errors.New("no binary data")
	ErrDataCorruption = errors.New("data corruption detected") // io.EOF or ".\r\n" reached before =yend
	ErrCrcMismatch    = errors.New("crc32 mismatch")
)

type streamFeeder interface {
	feed(in []byte) (consumed int, done bool, err error)
}

// Next reads from r until a complete response is decoded.
// If r is a net.Conn, the caller is responsible for settings deadlines.
func (d *Decoder) Next() (*Response, error) {
	response := &Response{
		hasStatusLine: !d.statusLineConsumed,
		dataFunc:      d.dataFunc,
	}

	if err := d.rb.feedUntilDone(d.r, response); err != nil {
		if !response.eof && errors.Is(err, io.EOF) {
			// r return EOF but end of NNTP response was not reached
			return nil, io.ErrUnexpectedEOF
		}
		if !errors.Is(err, io.EOF) {
			return nil, err
		}
	}

	return response, nil
}

type Format int

const (
	FormatUnknown Format = iota
	FormatYenc
	FormatUU
)

// State is the current Decoder State, the values refer to the previously seen
// characters in the stream, which influence how some sequences need to be handled.
//
// The shorthands represent:
// CR (\r), LF (\n), EQ (=), DT (.)
type State int

const (
	StateCRLF State = iota
	StateEQ
	StateCR
	StateNone
	StateCRLFDT
	StateCRLFDTCR
	StateCRLFEQ // may actually be "\r\n.=" in raw Decoder
)

// End is the State for incremental decoding, whether the end of the yEnc data was reached
type End int

const (
	EndNone    End = iota // end not reached
	EndControl            // \r\n=y sequence found, src points to byte after 'y'
	EndArticle            // \r\n.\r\n sequence found, src points to byte after last '\n'
)

var (
	errDestinationTooSmall = errors.New("destination must be at least the length of source")
)
