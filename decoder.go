package rapidyenc

import (
	"errors"
	"fmt"
	"io"
	"sync"
)

type Decoder struct {
	r                  io.Reader
	rb                 readBuffer
	statusLineConsumed bool // Has the caller already consumed the status line; if so trust that it is a multiline response
	dataFunc           func() []byte
	scratch            []byte // decode target when a response is streamed to a sink instead of buffered

	// Guards the request queue, which is the only state a second goroutine
	// touches: a sender calling Expect, a monitor calling Pending, a supervisor
	// calling ClearExpected. It is not held across decoding.
	mu       sync.Mutex
	pending  []pendingRequest // requests sent, responses not yet decoded
	inFlight *pendingRequest  // request whose response is being decoded now
}

// A request that has been sent and whose response has not been decoded yet.
//
// Held in the Decoder so a response cannot be paired with the wrong request. A queue
// kept alongside by the caller can fall out of step, and the damage that does is
// silent: one article's bytes written into another article's file at a plausible
// offset.
type pendingRequest struct {
	request any
	sink    io.Writer   // sequential sink, nil unless sinkAt is
	sinkAt  io.WriterAt // positional sink, written at the offset the headers declare
}

// decodeScratch returns a reusable buffer of exactly n bytes to decode into.
func (d *Decoder) decodeScratch(n int) []byte {
	if cap(d.scratch) < n {
		d.scratch = make([]byte, n)
	}
	return d.scratch[:n]
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

// Expect records that a request has been sent so its response can be paired with it.
// Calls must be in the order the requests were sent; the response is paired with the
// oldest unanswered request and returned as Response.Request.
//
// sink is where the decoded body goes, or nil to collect it into Response.Data as
// usual. An io.WriterAt receives the body at the offset the yEnc headers declare, so
// parts fetched out of order on separate connections each land in the right place. An
// io.Writer receives it sequentially. Either way Response.Data is left nil.
//
// Bodies larger than the read buffer are written in pieces. A failed write does not
// abort the response: the rest of it still has to be read or the connection is left
// mid-article, so the body is discarded and the failure reported as
// Response.SinkFailed and Response.SinkError once the response completes.
//
// uuencoded responses carry no offset for a part to be placed at, so a sink is
// ignored for those and Response.Data is populated as usual.
func (d *Decoder) Expect(request, sink any) error {
	pending := pendingRequest{request: request}

	switch w := sink.(type) {
	case nil:
	case io.WriterAt:
		pending.sinkAt = w
	case io.Writer:
		pending.sink = w
	default:
		return fmt.Errorf("sink must be an io.WriterAt, an io.Writer or nil, not %T", sink)
	}

	d.mu.Lock()
	d.pending = append(d.pending, pending)
	d.mu.Unlock()

	return nil
}

// ClearExpected forgets every request still awaiting a response, for a connection
// being reset. A response already being decoded keeps the request and sink it was
// paired with.
func (d *Decoder) ClearExpected() {
	d.mu.Lock()
	d.pending = nil
	d.mu.Unlock()
}

// Expected is how many requests recorded with Expect have not been answered yet,
// including the one whose response is arriving now. Counting only unpaired requests
// would report a pipelined connection as idle while it is still receiving, because
// the request is paired as soon as the first byte of its response shows up.
func (d *Decoder) Expected() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.inFlight != nil {
		return len(d.pending) + 1
	}

	return len(d.pending)
}

// Pending is the requests still awaiting a response, oldest first, including the one
// whose response is arriving now. A caller needs these to say what a connection is
// fetching and to hand the outstanding articles back to its own queue when a
// connection is reset.
func (d *Decoder) Pending() []any {
	d.mu.Lock()
	defer d.mu.Unlock()

	requests := make([]any, 0, len(d.pending)+1)
	if d.inFlight != nil {
		requests = append(requests, d.inFlight.request)
	}
	for _, pending := range d.pending {
		requests = append(requests, pending.request)
	}

	return requests
}

var (
	ErrDataMissing    = errors.New("no binary data")
	ErrDataCorruption = errors.New("data corruption detected") // io.EOF or ".\r\n" reached before =yend
	ErrCrcMismatch    = errors.New("crc32 mismatch")
)

// Next reads from r until a complete response is decoded.
// If r is a net.Conn, the caller is responsible for settings deadlines.
func (d *Decoder) Next() (*Response, error) {
	response := &Response{}

	// Responses come back in the order the requests went out, so the oldest
	// unanswered request belongs to this one
	d.mu.Lock()
	if len(d.pending) > 0 {
		pending := d.pending[0]
		d.pending[0] = pendingRequest{} // the backing array outlives the entry
		d.pending = d.pending[1:]
		d.inFlight = &pending
		response.Request = pending.request
		response.sink = pending.sink
		response.sinkAt = pending.sinkAt
	}
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		d.inFlight = nil
		d.mu.Unlock()
	}()

	if err := d.rb.feedUntilDone(d, d.r, response); err != nil {
		if !response.eof && errors.Is(err, io.EOF) {
			// r return EOF but end of NNTP response was not reached
			return nil, io.ErrUnexpectedEOF
		}
		if !errors.Is(err, io.EOF) {
			return response, err
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
