package decoder

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"fmt"
	"github.com/mnightingale/go-rapidyenc/crc32"
	"github.com/mnightingale/go-rapidyenc/encoder"
	"github.com/stretchr/testify/require"
	"io"
	"testing"
)

type decodeCase struct {
	raw string
	crc uint32
}

type decodeMultiCase struct {
	name  string
	cases []decodeCase
}

func TestSingle(t *testing.T) {
	cases := []decodeCase{
		{"foobar", 0x9EF61F95},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			raw := []byte(tc.raw)

			var b bytes.Buffer
			w := bufio.NewWriter(&b)
			w.Write(body(raw))
			err := w.Flush()
			if err != nil {
				t.Fatal(err)
			}

			d := NewDecoder(bytes.NewReader(b.Bytes()))
			decoded, err := d.ReadAll()
			require.NoError(t, err)
			require.Equal(t, tc.raw, string(decoded))
			require.Equal(t, tc.crc, d.CRC32())
		})
	}
}

func TestMultiple(t *testing.T) {
	cases := []decodeMultiCase{
		{
			name: "foobar-baz",
			cases: []decodeCase{
				{"foobar", 0x9EF61F95},
				{"baz", 0x78240498},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			w := bufio.NewWriter(&b)
			for _, c := range tc.cases {
				_, err := w.Write(body([]byte(c.raw)))
				require.NoError(t, err)
			}
			err := w.Flush()
			require.NoError(t, err)

			r := bytes.NewReader(b.Bytes())
			d := NewDecoder(r)

			for _, c := range tc.cases {
				decoded, err := d.ReadAll()
				require.NoError(t, err)
				require.Equal(t, c.raw, string(decoded))
				require.Equal(t, c.crc, d.CRC32())
				d.Reset()
			}
		})
	}
}

func BenchmarkSingle(b *testing.B) {
	raw := make([]byte, 768000)
	_, err := rand.Read(raw)
	require.NoError(b, err)
	encoded := body(raw)

	r := bytes.NewReader(encoded)

	for i := 0; i < b.N; i++ {
		d := NewDecoder(r)
		_, _ = io.ReadAll(d)
		r.Seek(0, io.SeekStart)
	}
}

func BenchmarkSingleReadAll(b *testing.B) {
	raw := make([]byte, 768000)
	_, err := rand.Read(raw)
	require.NoError(b, err)
	encoded := body(raw)

	r := bytes.NewReader(encoded)

	for i := 0; i < b.N; i++ {
		d := NewDecoder(r)
		_, _ = d.ReadAll()
		r.Seek(0, io.SeekStart)
	}
}

func body(raw []byte) []byte {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	enc := encoder.NewEncoder()
	encoded, _ := enc.Encode(raw)

	fmt.Fprintf(w, "=ybegin part=%d line=128 size=%d name=%s\r\n", 1, len(raw), "foo")
	fmt.Fprintf(w, "=ypart begin=%d end=%d\r\n", 1, len(raw)+1)
	w.Write(encoded)
	w.Write([]byte("\r\n"))
	fmt.Fprintf(w, "=yend size=%d part=%d pcrc32=%08x\r\n", len(raw), 1, crc32.Checksum(raw))
	fmt.Fprintf(w, ".\r\n")

	err := w.Flush()
	if err != nil {
		panic(err)
	}

	return b.Bytes()
}
