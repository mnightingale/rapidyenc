package rapidyenc

/*
#cgo darwin LDFLAGS: ${SRCDIR}/librapidyenc_darwin.a -lstdc++
#cgo windows,amd64 LDFLAGS: ${SRCDIR}/librapidyenc_windows_amd64.a -lstdc++
#cgo linux,amd64 LDFLAGS: ${SRCDIR}/librapidyenc_linux_amd64.a -lstdc++
#cgo linux,arm64 LDFLAGS: ${SRCDIR}/librapidyenc_linux_arm64.a -lstdc++
#include "rapidyenc.h"

// Like `rapidyenc_decode_incremental` but handle the pointer arithmetic
RapidYencDecoderEnd rapidyenc_decode_incremental_go(const void* src, void* dest, size_t src_length, size_t* n_src, size_t* n_dest, RapidYencDecoderState* state) {
    const void* in_ptr = src;
    void* out_ptr = dest;

    RapidYencDecoderEnd ended = rapidyenc_decode_incremental(&in_ptr, &out_ptr, src_length, state);
    *n_src = (uintptr_t)in_ptr - (uintptr_t)src;
    *n_dest = (uintptr_t)out_ptr - (uintptr_t)dest;

	return ended;
}
*/
import "C"
import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/text/transform"
	"hash"
	"hash/crc32"
	"io"
	"strconv"
	"sync"
	"unsafe"
)

var (
	decoderPool sync.Pool
)

// AcquireDecoder returns an empty Decoder instance from Decoder pool.
//
// The returned Decoder instance may be passed to ReleaseDecoder when it is
// no longer needed. This allows Decoder recycling, reduces GC pressure
// and usually improves performance.
func AcquireDecoder(r io.Reader) *Decoder {
	if v := decoderPool.Get(); v != nil {
		dec := v.(*Decoder)
		dec.Reset(r)
		return dec
	}
	return NewDecoder(r)
}

// ReleaseDecoder returns dec acquired via AcquireDecoder to Decoder pool.
//
// It is forbidden accessing dec and/or its members after returning
// it to Decoder pool.
func ReleaseDecoder(dec *Decoder) {
	dec.Reset(nil)
	decoderPool.Put(dec)
}

// Meta is the result of parsing the yEnc headers (ybegin, ypart, yend)

type Decoder struct {
	r    io.Reader
	Meta DecodedMeta

	body  bool
	begin bool
	part  bool
	end   bool
	crc   bool

	State       State
	format      Format
	actualSize  int64
	expectedCrc uint32
	hash        hash.Hash32

	err error

	// dst[dst0:dst1] contains bytes that have been transformed by Transform but
	// not yet copied out via Read.
	dst        []byte
	dst0, dst1 int

	// src[src0:src1] contains bytes that have been read from r but not
	// yet transformed through Transform.
	src        []byte
	src0, src1 int

	// transformComplete is whether the transformation is complete,
	// regardless of whether it was successful.
	transformComplete bool
}

const defaultBufSize = 4096

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{
		r:    r,
		dst:  make([]byte, defaultBufSize),
		src:  make([]byte, defaultBufSize),
		hash: crc32.NewIEEE(),
	}
}

var (
	ErrDataMissing    = errors.New("no binary data")
	ErrDataCorruption = errors.New("data corruption detected") // io.EOF or ".\r\n" reached before =yend
	ErrCrcMismatch    = errors.New("crc32 mismatch")
)

func (d *Decoder) Read(p []byte) (n int, err error) {
	for {
		// Copy out any transformed bytes and return the final error if we are done.
		if d.dst0 != d.dst1 {
			n = copy(p, d.dst[d.dst0:d.dst1])
			d.dst0 += n
			if d.dst0 == d.dst1 && d.transformComplete {
				return n, d.err
			}
			return n, nil
		} else if d.transformComplete {
			return 0, d.err
		}

		// Try to transform some source bytes, or to flush the transformer if we
		// are out of source bytes. We do this even if d.d.Read returned an error.
		// As the io.Reader documentation says, "process the n > 0 bytes returned
		// before considering the error".
		if d.src0 != d.src1 || d.err != nil {
			d.dst0 = 0
			d.dst1, n, err = d.Transform(d.dst, d.src[d.src0:d.src1], d.err == io.EOF)
			d.src0 += n

			switch {
			case err == nil:
				// The Transform call was successful; we are complete if we
				// cannot read more bytes into src.
				d.transformComplete = d.err != nil
				continue
			case errors.Is(err, transform.ErrShortDst) && (d.dst1 != 0 || n != 0):
				// Make room in dst by copying out, and try again.
				continue
			case errors.Is(err, transform.ErrShortSrc) && d.src1-d.src0 != len(d.src) && d.err == nil:
				// Read more bytes into src via the code below, and try again.
			default:
				d.transformComplete = true
				// The reader error (d.err) takes precedence over the
				// transformer error (err) unless d.err is nil or io.EOF.
				if d.err == nil || d.err == io.EOF {
					d.err = err
				}
				continue
			}
		}

		// Move any untransformed source bytes to the start of the buffer
		// and read more bytes.
		if d.src0 != 0 {
			d.src0, d.src1 = 0, copy(d.src, d.src[d.src0:d.src1])
		}
		n, d.err = d.r.Read(d.src[d.src1:])
		d.src1 += n
	}
}

// Transform starts by reading line-by-line to parse yEnc headers until it encounters yEnc data.
// It then incrementally decodes chunks of yEnc encoded data before returning to line-by-line processing
// for the footer and EOF pattern (.\r\n)
func (d *Decoder) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, err error) {
transform:
	if d.body && d.format == FormatYenc {
		nd, ns, end, _ := DecodeIncremental(dst[nDst:], src[nSrc:], &d.State)
		if nd > 0 {
			d.hash.Write(dst[nDst : nDst+nd])
			d.actualSize += int64(nd)
			nDst += nd
		}

		switch end {
		case EndControl:
			nSrc += ns - 2
			d.body = false
		case EndArticle:
			nSrc += ns - 3
			d.body = false
		default:
			if d.State == StateCRLFEQ {
				d.State = StateCRLF
				nSrc += ns - 1
			} else {
				nSrc += ns
			}
			return nDst, nSrc, transform.ErrShortSrc
		}
	}

	// Line by line processing
	for {
		// Article EOF
		if bytes.HasPrefix(src[nSrc:], []byte(".\r\n")) {
			atEOF = true
			nSrc += 3
			break
		}

		endOfLine := bytes.Index(src[nSrc:], []byte("\r\n"))
		if endOfLine == -1 {
			break
		}

		line := src[nSrc : nSrc+endOfLine+2]
		nSrc += endOfLine + 2

		if d.format == FormatUnknown {
			d.format = detectFormat(line)
		}

		switch d.format {
		case FormatYenc:
			d.processYenc(line)
			goto transform
		case FormatUU:
			// TODO: does not uudecode, for now just copies encoded data
			nDst += copy(dst[nDst:], line)
		}
	}

	if atEOF {
		d.Meta.Hash = d.hash.Sum32()
		if d.format == FormatUU {
			return nDst, nSrc, fmt.Errorf("[rapidyenc] uuencode not implemented")
		} else if !d.begin {
			err = fmt.Errorf("[rapidyenc] end of article without finding \"=begin\" header: %w", ErrDataMissing)
		} else if !d.end {
			err = fmt.Errorf("[rapidyenc] end of article without finding \"=yend\" trailer: %w", ErrDataCorruption)
		} else if (!d.part && d.Meta.FileSize != d.actualSize) || (d.part && d.Meta.PartSize != d.actualSize) {
			err = fmt.Errorf("[rapidyenc] expected size %d but got %d: %w", d.Meta.PartSize, d.actualSize, ErrDataCorruption)
		} else if d.crc && d.expectedCrc != d.Meta.Hash {
			err = fmt.Errorf("[rapidyenc] expected decoded data to have CRC32 hash %#08x but got %#08x: %w", d.expectedCrc, d.Meta.Hash, ErrCrcMismatch)
		} else {
			err = io.EOF
		}
		return nDst, nSrc, err
	} else {
		return nDst, nSrc, transform.ErrShortSrc
	}
}

func (d *Decoder) Reset(r io.Reader) {
	d.r = r
	d.src0, d.src1 = 0, 0
	d.dst0, d.dst1 = 0, 0

	d.body = false
	d.begin = false
	d.part = false
	d.end = false
	d.crc = false

	d.State = StateCRLF
	d.format = FormatUnknown
	d.actualSize = 0
	d.expectedCrc = 0
	d.hash.Reset()
	d.Meta = DecodedMeta{}

	d.err = nil
	d.transformComplete = false
}

func (d *Decoder) processYenc(line []byte) {
	var err error
	if bytes.HasPrefix(line, []byte("=ybegin ")) {
		d.begin = true
		d.Meta.FileSize, _ = extractInt(line, []byte(" size="))
		d.Meta.FileName, _ = extractString(line, []byte(" name="))
		if d.Meta.PartNumber, err = extractInt(line, []byte(" part=")); err != nil {
			d.body = true
			d.Meta.PartSize = d.Meta.FileSize
		}
		d.Meta.TotalParts, _ = extractInt(line, []byte(" total="))
	} else if bytes.HasPrefix(line, []byte("=ypart ")) {
		d.part = true
		d.body = true
		var begin int64
		if begin, err = extractInt(line, []byte(" begin=")); err == nil {
			d.Meta.Offset = begin - 1
		}
		if end, err := extractInt(line, []byte(" end=")); err == nil && begin > 0 {
			d.Meta.PartSize = end - d.Meta.Offset
		}
	} else if bytes.HasPrefix(line, []byte("=yend ")) {
		d.end = true
		if d.part {
			if crc, err := extractCRC(line, []byte(" pcrc32=")); err == nil {
				d.expectedCrc = crc
				d.crc = true
			}
		} else if crc, err := extractCRC(line, []byte(" crc32=")); err == nil {
			d.expectedCrc = crc
			d.crc = true
		}
		d.Meta.PartSize, _ = extractInt(line, []byte(" size="))
	}
}

func detectFormat(line []byte) Format {
	if bytes.HasPrefix(line, []byte("=ybegin ")) {
		return FormatYenc
	}

	length := len(line)
	if (length == 62 || length == 63) && (line[62] == '\n' || line[62] == '\r') && line[0] == 'M' {
		return FormatUU
	}

	if bytes.HasPrefix(line, []byte("begin ")) {
		ok := true
		pos := len("begin ")
		for pos < len(line) && line[pos] != ' ' {
			pos++

			if line[pos] < '0' || line[pos] > '7' {
				ok = false
				break
			}
		}
		if ok {
			return FormatUU
		}
	}

	return FormatUnknown
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
	StateCRLF     = State(C.RYDEC_STATE_CRLF)
	StateEQ       = State(C.RYDEC_STATE_EQ)
	StateCR       = State(C.RYDEC_STATE_CR)
	StateNone     = State(C.RYDEC_STATE_NONE)
	StateCRLFDT   = State(C.RYDEC_STATE_CRLFDT)
	StateCRLFDTCR = State(C.RYDEC_STATE_CRLFDTCR)
	StateCRLFEQ   = State(C.RYDEC_STATE_CRLFEQ) // may actually be "\r\n.=" in raw Decoder
)

// End is the State for incremental decoding, whether the end of the yEnc data was reached
type End int

const (
	EndNone    = End(C.RYDEC_END_NONE)    // end not reached
	EndControl = End(C.RYDEC_END_CONTROL) // \r\n=y sequence found, src points to byte after 'y'
	EndArticle = End(C.RYDEC_END_ARTICLE) // \r\n.\r\n sequence found, src points to byte after last '\n'
)

var (
	errDestinationTooSmall = errors.New("destination must be at least the length of source")
)

var decodeInitOnce sync.Once

func maybeInitDecode() {
	decodeInitOnce.Do(func() {
		C.rapidyenc_decode_init()
	})
}

// DecodeIncremental stops decoding when a yEnc/NNTP end sequence is found
func DecodeIncremental(dst, src []byte, state *State) (nDst, nSrc int, end End, err error) {
	maybeInitDecode()

	if len(src) == 0 {
		return 0, 0, EndNone, nil
	}

	if len(dst) < len(src) {
		return 0, 0, 0, errDestinationTooSmall
	}

	var cnSrc, cnDest C.size_t

	result := End(C.rapidyenc_decode_incremental_go(
		unsafe.Pointer(&src[0]),
		unsafe.Pointer(&dst[0]),
		C.size_t(len(src)),
		&cnSrc,
		&cnDest,
		(*C.RapidYencDecoderState)(unsafe.Pointer(state)),
	))

	return int(cnDest), int(cnSrc), result, nil
}

func extractString(data, substr []byte) (string, error) {
	start := bytes.Index(data, substr)
	if start == -1 {
		return "", fmt.Errorf("substr not found: %s", substr)
	}

	data = data[start+len(substr):]
	if end := bytes.IndexAny(data, "\x00\r\n"); end != -1 {
		return string(data[:end]), nil
	}

	return string(data), nil
}

func extractInt(data, substr []byte) (int64, error) {
	start := bytes.Index(data, substr)
	if start == -1 {
		return 0, fmt.Errorf("substr not found: %s", substr)
	}

	data = data[start+len(substr):]
	if end := bytes.IndexAny(data, "\x00\x20\r\n"); end != -1 {
		return strconv.ParseInt(string(data[:end]), 10, 64)
	}

	return strconv.ParseInt(string(data), 10, 64)
}

var (
	errCrcNotfound = errors.New("crc not found")
)

// extractCRC converts a hexadecimal representation of a crc32 hash
func extractCRC(data, substr []byte) (uint32, error) {
	start := bytes.Index(data, substr)
	if start == -1 {
		return 0, errCrcNotfound
	}

	data = data[start+len(substr):]
	end := bytes.IndexAny(data, "\x00\x20\r\n")
	if end != -1 {
		data = data[:end]
	}

	// Take up to the last 8 characters
	parsed := data[len(data)-min(8, len(data)):]

	// Left pad unexpected length with 0
	if len(parsed) != 8 {
		padded := []byte("00000000")
		copy(padded[8-len(parsed):], parsed)
		parsed = padded
	}

	_, err := hex.Decode(parsed, parsed)
	return binary.BigEndian.Uint32(parsed), err
}
