package rapidyenc

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"fmt"
	"github.com/stretchr/testify/require"
	"io"
	"testing"
)

func TestDecode(t *testing.T) {
	space := bytes.Repeat([]byte(" "), 800000)

	cases := []struct {
		name string
		raw  string
		crc  uint32
	}{
		{"foobar", "foobar", 0x9EF61F95},
		{"0x20", string(space), 0x31f365e7},
		{"special", "\x04\x04\x04\x04", 0xca2ee18a},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(tc.raw)

			encoded := body(raw)

			dec := AcquireDecoder(encoded)
			b := bytes.NewBuffer(nil)
			n, err := io.Copy(b, dec)
			require.Equal(t, int64(len(raw)), n)
			require.NoError(t, err)
			require.Equal(t, raw, b.Bytes())
			require.Equal(t, tc.crc, dec.Meta.Hash)
			require.Equal(t, int64(len(raw)), dec.Meta.End())
			ReleaseDecoder(dec)
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

			encoded := body(raw)

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

			dec := AcquireDecoder(r)
			b := bytes.NewBuffer(nil)
			n, err := io.Copy(b, dec)
			require.Equal(t, int64(len(raw)), n)
			require.NoError(t, err)
			require.Equal(t, raw, b.Bytes())
			require.Equal(t, int64(len(raw)), dec.Meta.End())
			ReleaseDecoder(dec)
		})
	}
}

func BenchmarkDecoder(b *testing.B) {
	raw := make([]byte, 1024*1024)
	_, err := rand.Read(raw)
	require.NoError(b, err)

	r := body(raw)

	dec := AcquireDecoder(r)

	for b.Loop() {
		_, err = io.Copy(io.Discard, dec)
		require.NoError(b, err)
		_, err = r.Seek(0, io.SeekStart)
		require.NoError(b, err)
		dec.Reset(r)
	}

	ReleaseDecoder(dec)
}

func body(raw []byte) io.ReadSeeker {
	w := new(bytes.Buffer)

	enc := NewEncoder(w, Meta{
		FileName: "filename",
		FileSize: int64(len(raw)),
		PartSize: int64(len(raw)),
	})

	if _, err := io.Copy(enc, bytes.NewReader(raw)); err != nil {
		panic(err)
	}
	if err := enc.Close(); err != nil {
		panic(err)
	}

	return bytes.NewReader(w.Bytes())
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
