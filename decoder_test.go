package rapidyenc

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecode(t *testing.T) {
	space := bytes.Repeat([]byte(" "), 800000)

	cases := []struct {
		name string
		raw  string
	}{
		{"foobar", "foobar"},
		{"0x20", string(space)},
		{"special", "\x04\x04\x04\x04"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(tc.raw)

			encoded, err := body(raw)
			require.NoError(t, err)

			dec := NewDecoder(encoded, WithStatusLineAlreadyRead())
			response, err := dec.Next()
			require.Equal(t, len(raw), len(response.Data))
			require.NoError(t, err)
			require.Equal(t, raw, response.Data)
			require.Equal(t, int64(len(raw)), response.Metadata.End())

		})
	}
}

func TestDecodePattern(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
	}{
		{"foobar", "A0B1C2D3E4F5G6H7"},
		{"alpha", "11111111222222223333333344444444555555556666666677777777888888889999999900000000"},
		{"special", "\u0004\u0004\u0004\u0004"},
	}

	length := 512
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := bytes.Repeat([]byte(tc.pattern), length/len(tc.pattern)+1)[:length]

			encoded, err := body(raw)
			require.NoError(t, err)

			dec := NewDecoder(encoded, WithStatusLineAlreadyRead())
			response, err := dec.Next()
			//require.Equal(t, int64(len(raw)), n)
			//require.NoError(t, err)
			require.Equal(t, raw, response.Data)
			//require.Equal(t, int64(len(raw)), dec.Meta.End())
		})
	}
}

func TestDecodeUU(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		length int
		crc    uint32
	}{
		{"logo_full", "testdata/logo_full.uu", 2184, 0x6BC2917D},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := os.Open(tc.path)
			require.NoError(t, err)
			defer f.Close()

			w := new(bytes.Buffer)
			io.Copy(w, f)
			w.WriteString(".\r\n")
			require.NoError(t, err)

			dec := NewDecoder(w, WithStatusLineAlreadyRead())
			response, err := dec.Next()
			require.NoError(t, err)
			require.Equal(t, tc.length, len(response.Data))
			require.Equal(t, tc.crc, response.Metadata.CRC)
		})
	}
}

// TestSplitReads splits "=y" header lines across reads
func TestSplitReads(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"foobar", "foobar"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(tc.raw)

			encoded, err := body(raw)
			require.NoError(t, err)

			r, w := io.Pipe()

			go func() {
				scanner := bufio.NewScanner(encoded)
				scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
					if bytes.Equal(data[:2], []byte("=y")) {
						return 1, []byte("="), nil
					}

					if line := bytes.Index(data, []byte("\r\n")); line != -1 {
						return line + 2, data[:line+2], nil
					}

					if atEOF {
						return 0, nil, io.EOF
					}

					return 0, nil, nil
				})

				for scanner.Scan() {
					if _, err := w.Write(scanner.Bytes()); err != nil {
						panic(err)
					}
				}

				if err := w.Close(); err != nil {
					panic(err)
				}
			}()

			dec := NewDecoder(r, WithStatusLineAlreadyRead())
			response, err := dec.Next()
			require.Equal(t, len(raw), len(response.Data))
			require.NoError(t, err)
			require.Equal(t, raw, response.Data)
			require.Equal(t, int64(len(raw)), response.Metadata.End())
		})
	}
}

func BenchmarkDecoder(b *testing.B) {
	raw := make([]byte, 1024*1024)
	_, err := rand.New(rand.NewSource(42)).Read(raw)
	require.NoError(b, err)

	r, err := body(raw)
	require.NoError(b, err)

	var bufferPool = sync.Pool{
		New: func() any {
			return make([]byte, 0, defaultReadBufSize)
		},
	}

	dec := NewDecoder(
		r,
		WithStatusLineAlreadyRead(),
		WithDataFunc(func() []byte {
			return bufferPool.Get().([]byte)
		}),
	)
	b.ResetTimer()
	for b.Loop() {
		response, err := dec.Next()
		require.NoError(b, err)
		bufferPool.Put(response.Data)
		_, err = r.Seek(0, io.SeekStart)
		require.NoError(b, err)
	}
}

func body(raw []byte) (io.ReadSeeker, error) {
	w := new(bytes.Buffer)

	enc, err := NewEncoder(w, Meta{
		FileName:   "filename",
		FileSize:   int64(len(raw)),
		PartSize:   int64(len(raw)),
		PartNumber: 1,
		TotalParts: 1,
	})
	if err != nil {
		return nil, err
	}

	if _, err := io.Copy(enc, bytes.NewReader(raw)); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	if _, err = w.Write([]byte(".\r\n")); err != nil {
		return nil, err
	}

	return bytes.NewReader(w.Bytes()), nil
}

func TestExtractString(t *testing.T) {
	cases := []struct {
		raw      string
		expected string
	}{
		{"", ""},
		{"foo", "foo"},
		{"name=bar", "name=bar"},
		{"foo bar", "foo bar"},
		{"before\x00after", "before"},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			b := []byte(fmt.Sprintf("=ybegin part=1 line=128 size=128 name=%s\r\n", tc.raw))
			i, err := extractString(b, []byte(" name="))
			require.NoError(t, err)
			require.Equal(t, tc.expected, i)
		})
	}
}

func TestExtractCRC(t *testing.T) {
	cases := []struct {
		raw      string
		expected uint32
	}{
		{"ffffffffa95d3e50", 0xa95d3e50},
		{"fffffffa95d3e50", 0xa95d3e50},
		{"ffffffa95d3e50", 0xa95d3e50},
		{"fffffa95d3e50", 0xa95d3e50},
		{"ffffa95d3e50", 0xa95d3e50},
		{"fffa95d3e50", 0xa95d3e50},
		{"ffa95d3e50", 0xa95d3e50},
		{"fa95d3e50", 0xa95d3e50},
		{"a95d3e50", 0xa95d3e50},
		{"a95d3e5", 0xa95d3e5},
		{"a95d3e", 0xa95d3e},
		{"a95d3", 0xa95d3},
		{"a95d", 0xa95d},
		{"a95", 0xa95},
		{"a9", 0xa9},
		{"a", 0xa},
		{"", 0},
		{"12345678 ", 0x12345678}, // space at end
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			b := []byte(fmt.Sprintf("pcrc32=%s", tc.raw))
			i, err := extractCRC(b, []byte("pcrc32="))
			require.NoError(t, err)
			require.Equal(t, tc.expected, i)
		})
	}
}

func TestHelp(t *testing.T) {
	lines := []string{
		"100 Help follows.",
		" ARTICLE [msgid|number]",
		" BODY [msgid|number]",
		" HEAD [msgid|number]",
		" GROUP [newsgroup]",
		" STAT [msgid|number]",
		" OVER [range]",
		" XOVER [range]",
		" XHDR [header] [range]",
		" POST",
		" IHAVE [msgid]",
		" LIST",
		" MODE [reader]",
		" DATE",
		" XZVER [range]",
		" XZHDR [header] [range|msgid]",
		" HELP",
		" QUIT",
		".\r\n",
	}

	encoded := strings.NewReader(strings.Join(lines, "\r\n"))

	dec := NewDecoder(encoded)
	response, err := dec.Next()
	require.NoError(t, err)
	for _, line := range lines[1 : len(lines)-1] {
		require.Contains(t, response.Metadata.Lines, line)
	}
}

// TestXZVER Astraweb style yenc(deflate(overview))
func TestXZVERYenc(t *testing.T) {
	cases := []struct {
		raw string
	}{
		{"hello world"},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			buf := bytes.NewBuffer(nil)
			compressor, err := zlib.NewWriterLevel(buf, zlib.BestSpeed)
			require.NoError(t, err)
			_, err = compressor.Write([]byte(tc.raw))
			require.NoError(t, err)
			err = compressor.Close()
			require.NoError(t, err)

			encoded := bytes.NewBuffer([]byte("224 Overview follows\r\n=ybegin line=128 size=-1\r\n"))
			enc, err := NewEncoder(encoded, Meta{}, WithRaw())
			require.NoError(t, err)
			_, err = io.Copy(enc, bytes.NewReader(buf.Bytes()))
			require.NoError(t, err)
			err = enc.Close()
			require.NoError(t, err)
			_, err = fmt.Fprintf(encoded, "\r\n=yend crc32=%08x\r\n.\r\n", enc.crc)
			require.NoError(t, err)

			dec := NewDecoder(bytes.NewReader(encoded.Bytes()))
			response, err := dec.Next()
			require.NoError(t, err)
			require.Equal(t, buf.Bytes(), response.Data)
		})
	}
}
