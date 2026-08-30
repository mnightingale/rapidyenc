package rapidyenc

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// uuLine encodes one uuencoded line: a length byte followed by 4 characters
// per 3 bytes of input.
func uuLine(data []byte) string {
	enc := func(b byte) byte { return ' ' + (b & 0x3F) }
	out := []byte{enc(byte(len(data)))}
	for i := 0; i < len(data); i += 3 {
		var g [3]byte
		copy(g[:], data[i:])
		out = append(out,
			enc(g[0]>>2),
			enc(g[0]<<4|g[1]>>4),
			enc(g[1]<<2|g[2]>>6),
			enc(g[2]))
	}
	return string(out)
}

// decodeUU builds a uuencoded body. The leading lines are full length so the
// decoder's heuristic recognises the format before it reaches any bad line.
func decodeUU(t *testing.T, extra ...string) *Response {
	t.Helper()
	raw := bytes.Repeat([]byte("abcdefghi"), 15) // 135 bytes = 3 full lines
	var lines []string
	for i := 0; i < len(raw); i += 45 {
		lines = append(lines, uuLine(raw[i:min(i+45, len(raw))]))
	}
	lines = append(lines, extra...)

	body := "222 Body follows\r\nbegin 644 test.bin\r\n" +
		strings.Join(lines, "\r\n") + "\r\n`\r\nend\r\n.\r\n"
	dec := NewDecoder(strings.NewReader(body))
	response, err := dec.Next()
	require.NoError(t, err)
	require.Equal(t, FormatUU, response.Metadata.Format)
	return response
}

func TestBadDataCleanUU(t *testing.T) {
	response := decodeUU(t)
	require.Equal(t, bytes.Repeat([]byte("abcdefghi"), 15), response.Data)
	require.False(t, response.Metadata.BadData)
}

// A length byte declaring more data than the line can possibly hold.
func TestBadDataInvalidLineLength(t *testing.T) {
	full := uuLine(bytes.Repeat([]byte("a"), 45))
	response := decodeUU(t, full[:20])
	require.True(t, response.Metadata.BadData)
}

// A line whose declared length passes the cheap length check but whose
// characters run out before that many bytes have been decoded.
func TestBadDataTruncatedGroup(t *testing.T) {
	full := uuLine(bytes.Repeat([]byte("a"), 45))
	response := decodeUU(t, full[:52])
	require.True(t, response.Metadata.BadData)
}

func TestBadDataUnsetForYenc(t *testing.T) {
	raw := bytes.Repeat([]byte("abcdefgh"), 500)
	encoded, err := body(raw)
	require.NoError(t, err)

	dec := NewDecoder(encoded, WithStatusLineAlreadyRead())
	response, err := dec.Next()
	require.NoError(t, err)
	require.Equal(t, raw, response.Data)
	require.False(t, response.Metadata.BadData)
}

func TestBadDataUnsetForValidUUFile(t *testing.T) {
	f, err := os.Open("testdata/logo_full.uu")
	require.NoError(t, err)
	defer f.Close()

	w := new(bytes.Buffer)
	_, err = io.Copy(w, f)
	require.NoError(t, err)
	w.WriteString(".\r\n")

	dec := NewDecoder(w, WithStatusLineAlreadyRead())
	response, err := dec.Next()
	require.NoError(t, err)
	require.False(t, response.Metadata.BadData)
}
