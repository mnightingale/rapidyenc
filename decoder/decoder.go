package decoder

/*
#cgo CFLAGS: -I${SRCDIR}/../rapidyenc
#cgo LDFLAGS: -L${SRCDIR}/../ -lrapidyenc
#include "rapidyenc.h"
*/
import "C"
import (
	"bytes"
	"errors"
	"fmt"
	"golang.org/x/text/transform"
	"hash"
	"hash/crc32"
	"io"
	"strconv"
	"unsafe"
)

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{
		r: r,
		Decode: &Decode{
			outCrc: crc32.New(crc32.IEEETable),
		},
		dst: make([]byte, defaultBufSize),
		src: make([]byte, defaultBufSize),
	}
}

type Decoder struct {
	r io.Reader
	*Decode

	err error

	// dst[dst0:dst1] contains bytes that have been transformed by t but
	// not yet copied out via Read.
	dst        []byte
	dst0, dst1 int

	// src[src0:src1] contains bytes that have been read from r but not
	// yet transformed through t.
	src        []byte
	src0, src1 int

	// transformComplete is whether the transformation is complete,
	// regardless of whether or not it was successful.
	transformComplete bool
}

// ReadAll has similar behaviour to io.ReadAll.
// It tries to be a bit smarter about growing the byte slice based on the parsed expected article size
func (d *Decoder) ReadAll() ([]byte, error) {
	b := make([]byte, 0, defaultBodySize)

	length := 0

	for {
		if length == cap(b) {
			// Add more capacity.
			extend := d.ArticleSize() - int64(length)
			if extend <= 0 || extend > maxBodySize {
				b = append(b, 0)[:cap(b)]
			} else {
				b = append(b, make([]byte, extend)...)
			}
		}
		n, err := d.Read(b[length:cap(b)])
		length += n
		b = b[:length]
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return b, err
		}
	}
}

func (d *Decoder) Read(p []byte) (int, error) {
	n, err := 0, error(nil)
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

const defaultBufSize = 4096 * 8
const defaultBodySize = 4096
const maxBodySize = 10 * 1024 * 1024 // maximum size to initially extend output buffer to

var (
	ErrDataMissing    = errors.New("no binary data")
	ErrDataIncomplete = errors.New("incomplete data") // .\r\n reached before =yend
	ErrSizeMismatch   = errors.New("size mismatch")
	ErrCrcMismatch    = errors.New("crc32 mismatch")
)

// Transform starts by reading line-by-line to parse yEnc headers until it encounters yEnc data.
// It then incrementally decodes chunks of yEnc encoded data before returning to line-by-line processing
// for the footer and EOF pattern (.\r\n)
func (d *Decoder) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, err error) {
	if d.body && d.format == FormatYenc {
		ns, nd, end := decodeIncremental(dst, src, &d.state)
		if nd > 0 {
			d.outCrc.Write(dst[nDst : nDst+nd])
			d.outSize += int64(nd)
			nDst += nd
		}
		nSrc += ns

		if end {
			d.body = false
		} else {
			return nDst, nSrc, transform.ErrShortSrc
		}
	}

	// Line by line processing
	for {
		// Article EOF
		if bytes.HasPrefix(src[nSrc:], []byte(".\r\n")) {
			if d.format == FormatUU {
				return nDst, nSrc + 3, fmt.Errorf("uuencode not implemented")
			} else if !d.begin {
				err = ErrDataMissing
			} else if !d.end {
				err = ErrDataIncomplete
			} else if (!d.part && d.size != d.endSize) || (d.endSize != d.outSize) {
				err = ErrSizeMismatch
			} else if d.crc && d.expectedCrc != d.outCrc.Sum32() {
				err = ErrCrcMismatch
			} else {
				err = io.EOF
			}
			return nDst, nSrc + 3, err
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

		if d.format == FormatYenc {
			d.processYenc(line)
			if d.body {
				ns, nd, end := decodeIncremental(dst[nDst:], src[nSrc:], &d.state)
				if nd > 0 {
					d.outCrc.Write(dst[nDst : nDst+nd])
					d.outSize += int64(nd)
					nDst += nd
				}
				nSrc += ns

				if end {
					d.body = false
					continue
				} else {
					return nDst, nSrc, transform.ErrShortSrc
				}
			}
		}
	}

	return nDst, nSrc, transform.ErrShortSrc
}

func (d *Decoder) Reset() {
	d.body = false
	d.begin = false
	d.part = false
	d.end = false
	d.crc = false

	d.state = StateCRLF
	d.format = FormatUnknown
	d.beginPos = 0
	d.endPos = 0
	d.size = 0
	d.endSize = 0
	d.outSize = 0
	d.expectedCrc = 0
	d.outCrc = crc32.New(crc32.IEEETable)
	d.filename = ""

	d.err = nil
	d.transformComplete = false
}

// Decode stores state of what has been decoded
type Decode struct {
	body  bool
	begin bool
	part  bool
	end   bool
	crc   bool

	state       State
	format      Format
	beginPos    int64
	endPos      int64
	size        int64
	endSize     int64
	outSize     int64
	expectedCrc uint32
	outCrc      hash.Hash32
	filename    string
}

func (d *Decode) Offset() int64 {
	return d.beginPos - 1
}

func (d *Decode) ArticleSize() int64 {
	return d.endPos - d.beginPos + 1
}

func (d *Decode) Filename() string {
	return d.filename
}

func (d *Decode) Filesize() int64 {
	return d.size
}

func (d *Decode) CRC32() uint32 {
	return d.outCrc.Sum32()
}

func (d *Decode) processYenc(line []byte) {
	if bytes.HasPrefix(line, []byte("=ybegin ")) {
		d.begin = true
		d.size, _ = extractInt(line, []byte(" size="))
		d.filename, _ = extractString(line, []byte(" name="))
		if _, err := extractInt(line, []byte(" part=")); err != nil {
			d.body = true
			d.beginPos = 1
			d.endPos = d.size
		}
	} else if bytes.HasPrefix(line, []byte("=ypart ")) {
		d.part = true
		d.body = true
		d.beginPos, _ = extractInt(line, []byte(" begin="))
		d.endPos, _ = extractInt(line, []byte(" end="))
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
		d.endSize, _ = extractInt(line, []byte(" size="))
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

type Format uint32

const (
	FormatUnknown Format = iota
	FormatYenc
	FormatUU
)

func extractString(data, substr []byte) (string, error) {
	start := bytes.Index(data, substr)
	if start != -1 {
		start += len(substr)
		end := start
		for ; end < len(data); end++ {
			if data[end] == 0x00 || data[end] == '\r' || data[end] == '\n' {
				break
			}
		}

		return string(data[start:end]), nil
	}

	return "", fmt.Errorf("substr not found: %s", substr)
}

func extractInt(data, substr []byte) (int64, error) {
	start := bytes.Index(data, substr)
	if start != -1 {
		start += len(substr)
		end := start
		for ; end < len(data); end++ {
			if data[end] == 0x00 || data[end] == '\r' || data[end] == '\n' || data[end] == ' ' {
				break
			}
		}
		return strconv.ParseInt(string(data[start:end]), 10, 64)
	}

	return 0, fmt.Errorf("substr not found: %s", substr)
}

func extractCRC(data, substr []byte) (uint32, error) {
	start := bytes.Index(data, substr)
	if start != -1 {
		start += len(substr)
		end := start + 8
		if end <= len(data) {
			if size, err := strconv.ParseUint(string(data[start:end]), 16, 32); err == nil {
				return uint32(size), nil
			}
		}
	}

	return 0, fmt.Errorf("crc not found: %s", substr)
}

func init() {
	C.rapidyenc_decode_init()
}

// State is the current decoder state, for incremental decoding
// The values here refer to the previously seen characters in the stream, which influence how some sequences need to be handled
// The shorthands represent:
// CR (\r), LF (\n), EQ (=), DT (.)
type State int

const (
	StateCRLF     State = C.RYDEC_STATE_CRLF
	StateEQ       State = C.RYDEC_STATE_EQ
	StateCR       State = C.RYDEC_STATE_CR
	StateNone     State = C.RYDEC_STATE_NONE
	StateCRLFDT   State = C.RYDEC_STATE_CRLFDT
	StateCRLFDTCR State = C.RYDEC_STATE_CRLFDTCR
	StateCRLFEQ   State = C.RYDEC_STATE_CRLFEQ // may actually be "\r\n.=" in raw decoder
)

// end is the state for incremental decoding, whether the end of the yEnc data was reached
const (
	endNone    = C.RYDEC_END_NONE    // end not reached
	endControl = C.RYDEC_END_CONTROL // \r\n=y sequence found, src points to byte after 'y'
	endArticle = C.RYDEC_END_ARTICLE // \r\n.\r\n sequence found, src points to byte after last '\n'
)

var (
	ErrDestinationTooSmall = errors.New("destination must be at least the length of source")
)

// decodeIncremental stops decoding when a yEnc/NNTP end sequence is found
func decodeIncremental(dst, src []byte, state *State) (int, int, bool) {
	if len(dst) < len(src) {
		panic(ErrDestinationTooSmall)
	}

	srcPointer := uintptr(unsafe.Pointer(&src[0]))
	dstPointer := uintptr(unsafe.Pointer(&dst[0]))

	end := C.rapidyenc_decode_incremental(
		(*unsafe.Pointer)(unsafe.Pointer(&srcPointer)),
		(*unsafe.Pointer)(unsafe.Pointer(&dstPointer)),
		C.size_t(len(src)),
		(*C.RapidYencDecoderState)(unsafe.Pointer(state)),
	)

	nSrc := int(srcPointer - uintptr(unsafe.Pointer(&src[0])))
	nDst := int(dstPointer - uintptr(unsafe.Pointer(&dst[0])))

	if end == endControl {
		nSrc -= len("=y")
	} else if end == endArticle {
		nSrc -= len("./r/n")
	}

	return nSrc, nDst, end != endNone
}
