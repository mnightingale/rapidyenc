package rapidyenc

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"strconv"
	"unicode"
)

type Response struct {
	Metadata      ResponseMeta
	Data          []byte
	hasStatusLine bool
	state         State
	eof           bool
	body          bool
	hasPart       bool
	hasBegin      bool
	hasEnd        bool
	hasCrc        bool
	hasEmptyLine  bool // for article requests has the empty line separating headers and body been seen
	hasBadData    bool // invalid line lengths for uu decoding; some data lost
	dataFunc      func() []byte
}

const nntpBody = 222
const nntpArtiicle = 220
const nntpHead = 221
const nntpCapabilities = 101

// feed consumes raw NNTP protocol bytes from buf, writing any decoded payload bytes to r.Data.
func (r *Response) feed(buf []byte) (consumed int, done bool, err error) {
	n, err := r.decode(buf)
	r.Metadata.BytesConsumed += int64(n)

	if err != nil {
		return n, false, err
	}
	if r.eof {
		return n, true, r.metaError()
	}
	return n, false, nil
}

func (r *Response) metaError() error {
	if !isMultiline(r.Metadata.StatusCode) {
		return nil
	}
	if r.Metadata.Format == FormatUU {
		return nil
	}
	if !r.hasBegin {
		return fmt.Errorf("[rapidyenc] end of article without finding \"=ybegin\" header: %w", ErrDataMissing)
	}
	if !r.hasEnd {
		return fmt.Errorf("[rapidyenc] end of article without finding \"=yend\" trailer: %w", ErrDataCorruption)
	}
	if (!r.hasPart && r.Metadata.FileSize != r.Metadata.BytesProduced) || (r.hasPart && r.Metadata.PartSize != r.Metadata.BytesProduced) {
		return fmt.Errorf("[rapidyenc] expected size %d but got %d: %w", r.Metadata.PartSize, r.Metadata.BytesProduced, ErrDataCorruption)
	}
	if r.hasCrc && r.Metadata.ExpectedCRC != r.Metadata.CRC {
		return fmt.Errorf("[rapidyenc] expected decoded data to have CRC32 hash %#08x but got %#08x: %w", r.Metadata.ExpectedCRC, r.Metadata.CRC, ErrCrcMismatch)
	}
	return nil
}

func (r *Response) decode(buf []byte) (read int, err error) {
	if r.body && r.Metadata.Format == FormatYenc {
		n, err := r.decodeYenc(buf)
		if err != nil {
			return int(n), err
		}
		read += int(n)
		buf = buf[n:]
		if r.body {
			return int(n), err
		}
	}

	// Line by line processing
	if !r.body {
		var line []byte
		var found bool
		for {
			if line, buf, found = bytes.Cut(buf, []byte("\r\n")); !found {
				break
			}
			read += len(line) + 2

			if bytes.Equal(line, []byte(".")) {
				r.eof = true
				break
			}

			if r.Metadata.Format == FormatUnknown {
				if r.hasStatusLine && r.Metadata.StatusCode == 0 && len(line) >= 3 {
					r.Metadata.Message = string(line)
					r.Metadata.StatusCode, err = strconv.Atoi(string(line[:3]))
					if err != nil || !isMultiline(r.Metadata.StatusCode) {
						r.eof = true
						break
					}
					continue
				}
				r.detectFormat(line)
			}

			switch r.Metadata.Format {
			case FormatUnknown:
				r.Metadata.Lines = append(r.Metadata.Lines, string(line))
			case FormatYenc:
				r.processYencHeader(line)
				if r.body {
					n, err := r.decodeYenc(buf)
					read += int(n)
					buf = buf[n:]
					if err != nil {
						return read, err
					}
					if r.body {
						// Still decoding, need more data
						return read, nil
					}
					// =ypart was encountered, switch to body decoding
				}
			case FormatUU:
				err := r.decodeUU(line)
				if err != nil {
					return read, err
				}
			}
		}
	}

	return read, nil
}

func (r *Response) detectFormat(line []byte) {
	if r.hasStatusLine && r.Metadata.StatusCode != nntpBody && r.Metadata.StatusCode != nntpArtiicle {
		return
	}

	if len(line) == 0 {
		r.hasEmptyLine = true
		return
	}

	// YEnc detection
	if bytes.HasPrefix(line, []byte("=ybegin ")) {
		r.Metadata.Format = FormatYenc
		return
	}

	// UUEncode detection: 60 or 61 chars, starts with 'M'
	if (len(line) == 60 || len(line) == 61) && line[0] == 'M' {
		r.Metadata.Format = FormatUU
		return
	}

	// UUEncode alternative header form: "begin "
	if bytes.HasPrefix(line, []byte("begin ")) {
		// Skip leading spaces
		line = bytes.TrimLeft(line[6:], " ")

		// Extract the next token (permission part)
		perms, found := bytes.CutPrefix(line, []byte(" "))
		if !found {
			return
		}

		// Check all characters are between '0' and '7'
		valid := true
		for _, c := range perms {
			if c < '0' || c > '7' {
				valid = false
				break
			}
		}

		if valid {
			r.Metadata.Format = FormatUU
		}
		return
	}

	// Remove dot stuffing
	if bytes.HasPrefix(line, []byte("..")) {
		line = line[1:]
	}

	// Multipart UU with a short final part
	if len(line) <= 1 {
		return
	}

	// For Article responses only consider after the headers
	if r.hasStatusLine && !(r.Metadata.StatusCode == nntpBody || (r.Metadata.StatusCode == nntpArtiicle && r.hasEmptyLine)) {
		return
	}

	first := line[0]
	n := len(line)

	for _, length := range []int{
		decodeUUCharWorkaround(first),
		decodeUUChar(first),
	} {
		if n < length {
			continue
		}

		body := line[1:length]
		padding := line[length:]

		if !allInASCIIRange(body, 32, 96) || !onlySpaceOrBacktick(padding) {
			continue
		}

		// Probably UU
		r.Metadata.Format = FormatUU
		r.body = true
		return
	}
}

func allInASCIIRange(b []byte, lo, hi byte) bool {
	for _, c := range b {
		if c < lo || c > hi {
			return false
		}
	}
	return true
}

func onlySpaceOrBacktick(b []byte) bool {
	for _, c := range b {
		if c != ' ' && c != '`' {
			return false
		}
	}
	return true
}

func decodeUUCharWorkaround(c byte) int {
	return int(((int(c)-32)&63)*4+5) / 3
}

func decodeUUChar(c byte) int {
	if c == '`' {
		return 0
	}
	return int((c - ' ') & 0x3F)
}

func isMultiline(code int) bool {
	return code == nntpBody || code == nntpArtiicle || code == nntpHead || code == nntpCapabilities
}

const (
	yencMaxPartSize   = 10 * 1024 * 1024 // 10 MiB
	yencMinBufferSize = 1024
)

func (r *Response) computeExpectedSize(inputLen int) int {
	// Allocate output buffer on first decode call
	// Use size from headers, capped at yencMaxPartSize for safety
	base := r.Metadata.PartSize
	if base <= 0 {
		base = r.Metadata.FileSize
	}
	expected := int(base) + 64 // small margin to see the end of yEnc data
	// Round up to next multiple of defaultReadBufSize
	expected = ((expected + defaultReadBufSize - 1) / defaultReadBufSize) * defaultReadBufSize
	// Add an extra chunk so we should never need to resize
	expected += defaultReadBufSize

	expected = max(expected, yencMinBufferSize)
	expected = min(expected, yencMaxPartSize)
	expected = max(expected, inputLen)

	return expected
}

// ensureData ensures r.Data has enough capacity for inputLen additional decoded bytes.
// On first call, sizes the buffer based on yEnc header metadata.
func (r *Response) ensureData(inputLen int) {
	if r.Data == nil {
		expected := r.computeExpectedSize(inputLen)
		if r.dataFunc != nil {
			r.Data = r.dataFunc()[:0]
			r.grow(expected)
			return
		}

		r.Data = make([]byte, 0, expected)
		return
	}

	r.grow(len(r.Data) + inputLen)
}

// grow extends r.Data to at least n capacity.
func (r *Response) grow(n int) {
	if cap(r.Data) < n {
		if n < 2*cap(r.Data) {
			// Grow to 2x current capacity
			n = 2 * cap(r.Data)
		}
		newData := append([]byte(nil), make([]byte, n)...)
		i := copy(newData, r.Data)
		r.Data = newData[:i]
	}
}

func (r *Response) decodeYenc(buf []byte) (consumed int, err error) {
	if len(buf) == 0 {
		return 0, nil
	}

	r.ensureData(len(buf))

	offset := len(r.Data)
	out := r.Data[offset : offset+len(buf)]

	var produced int
	var decoded []byte
	var end End

	consumed, decoded, r.state, end, err = decodeIncremental(out, buf, r.state)
	produced = len(decoded)
	if produced > 0 {
		r.Data = r.Data[:offset+produced]
		r.Metadata.CRC = crc32.Update(r.Metadata.CRC, crc32.IEEETable, decoded)
		r.Metadata.BytesProduced += int64(produced)
	}

	switch end {
	case EndNone:
		if r.state == StateCRLFEQ {
			// Special case: found "\r\n=" but no more data - might be start of =yend
			r.state = StateCRLF
			consumed -= 1 // Back up to allow =yend detection
		}
	case EndControl:
		// Found "\r\n=y" - likely =yend line, exit body mode
		r.body = false
		consumed -= 2 // Back up to include "=y" for header processing
	case EndArticle:
		// Found ".\r\n" - NNTP article terminator, exit body mode
		r.body = false
		consumed -= 3 // Back up to include ".\r\n" for terminator detection
	}

	return consumed, nil
}

func (r *Response) decodeUU(line []byte) error {
	// Detect 'begin' line and extract filename
	if !r.body {
		if bytes.HasPrefix(line, []byte("begin ")) {
			line = line[6:]
			line = bytes.TrimLeft(line, " ")                 // skip leading spaces
			line = bytes.TrimLeftFunc(line, unicode.IsDigit) // skip permissions
			line = bytes.TrimLeft(line, " ")                 // skip spaces after permissions

			// The rest of the line is the filename
			r.Metadata.FileName = string(line)

			r.body = true
			return nil
		}

		// Begin missing but looks like UUEncode: 60 or 61 chars, starts with 'M'
		if (len(line) == 60 || len(line) == 61) && line[0] == 'M' {
			r.body = true
		}
	}

	// Detect 'end' line
	if r.body && (bytes.Equal(line, []byte("`")) || bytes.Equal(line, []byte("end"))) {
		r.body = false
		r.Metadata.FileSize = r.Metadata.BytesProduced
		return nil
	}

	// Decode body lines
	if r.body {
		// Ignore junk
		if len(line) == 0 || bytes.Equal(line, []byte("-- ")) || bytes.HasPrefix(line, []byte("Posted via ")) {
			return nil
		}

		// Remove dot stuffing
		if bytes.HasPrefix(line, []byte("..")) {
			line = line[1:]
		}

		effLen := decodeUUChar(line[0])
		if effLen > len(line)-1 {
			// Workaround for broken uuencoders by Fredrik Lundh
			effLen = decodeUUCharWorkaround(line[0])
			if effLen > len(line)-1 {
				// Bad line
				r.hasBadData = true
				return nil
			}
		}

		line = line[1:] // skip length byte
		reader := bytes.NewReader(line)

		decoded := make([]byte, 0, effLen)

		uu := func() (byte, error) {
			b, err := reader.ReadByte()
			if err != nil {
				return 0, err
			}
			return byte(decodeUUChar(b)), nil
		}

		var err error
		var c0, c1, c2, c3 byte

		for ; effLen > 0 && reader.Len() >= 4; effLen -= 3 {
			chunk := min(effLen, 3)
			if c0, err = uu(); err != nil {
				break
			}
			if c1, err = uu(); err != nil {
				break
			}
			c2 = 0
			decoded = append(decoded, c0<<2|c1>>4)

			if chunk > 1 {
				if c2, err = uu(); err != nil {
					break
				}
				decoded = append(decoded, c1<<4|c2>>2)
			}

			if chunk > 2 {
				if c3, err = uu(); err != nil {
					break
				}
				decoded = append(decoded, c2<<6|c3)
			}
		}

		if len(decoded) > 0 {
			r.Data = append(r.Data, decoded...)
			r.Metadata.BytesProduced += int64(len(decoded))
			r.Metadata.CRC = crc32.Update(r.Metadata.CRC, crc32.IEEETable, decoded)
		}
	}

	return nil
}

func (r *Response) processYencHeader(line []byte) {
	var err error
	if bytes.HasPrefix(line, []byte("=ybegin ")) {
		r.hasBegin = true
		line = line[len("=ybegin"):]
		r.Metadata.FileSize, _ = extractInt(line, []byte(" size="))
		r.Metadata.FileName, _ = extractString(line, []byte(" name="))
		if r.Metadata.PartNumber, err = extractInt(line, []byte(" part=")); err != nil {
			// Not multi-part, so body starts immediately after =ybegin
			r.body = true
			r.Metadata.PartSize = r.Metadata.FileSize
		}
		r.Metadata.TotalParts, _ = extractInt(line, []byte(" total="))
	} else if bytes.HasPrefix(line, []byte("=ypart ")) {
		// =ypart signals start of body data in multi-part files
		r.hasPart = true
		r.body = true
		line = line[len("=ypart"):]
		var begin int64
		// Convert from 1-based to 0-based indexing
		if begin, err = extractInt(line, []byte(" begin=")); err == nil {
			r.Metadata.Offset = begin - 1
		}
		if end, err := extractInt(line, []byte(" end=")); err == nil && end >= begin {
			r.Metadata.PartSize = end - r.Metadata.Offset
		}
	} else if bytes.HasPrefix(line, []byte("=yend ")) {
		r.hasEnd = true
		line = line[len("=yend"):]
		if crc, err := extractCRC(line, []byte(" pcrc32=")); err == nil {
			r.Metadata.ExpectedCRC = crc
			r.hasCrc = true
		} else if crc, err := extractCRC(line, []byte(" crc32=")); err == nil {
			r.Metadata.ExpectedCRC = crc
			r.hasCrc = true
		}
		r.Metadata.EndSize, _ = extractInt(line, []byte(" size="))
	}
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
