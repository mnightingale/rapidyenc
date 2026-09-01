package rapidyenc

import (
	"bytes"
	"crypto/rand"
	"hash/crc32"
	"io"
	randv2 "math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

type encoderCase struct {
	name     string
	input    []byte
	expected []byte
}

func TestEncoderSimple(t *testing.T) {
	cases := []encoderCase{
		{"NUL", []byte("\x00"), []byte("\x2a")},
		{"SPACE", []byte("\x20"), []byte("\x4a")},
		{"ESCAPE", []byte("\xF6"), []byte("\x3D\x60")},                // Ends with <space> so must be escaped
		{"ESCAPE_NOT_FIRST", []byte("H\xF6"), []byte("\x72\x3D\x60")}, // Ends with <space> and not the first column, so must be escaped
		{"Hello World", []byte("Hello World"), []byte("\x72\x8F\x96\x96\x99\x4A\x81\x99\x9C\x96\x8E")},
		{"3DD4", []byte("\x3D\xD4"), []byte("\x67\xFE")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := bytes.NewReader(tc.input)

			encoded := new(bytes.Buffer)
			w, err := NewEncoder(encoded, Meta{
				FileName:   "filename",
				FileSize:   int64(len(tc.input)),
				PartSize:   int64(len(tc.input)),
				PartNumber: 1,
				TotalParts: 1,
			})
			require.NoError(t, err)
			_, err = io.Copy(w, input)
			require.NoError(t, err)
			err = w.Close()
			require.NoError(t, err)

			// Check contains the expected encoded value
			require.Contains(t, string(encoded.Bytes()), string(slices.Concat([]byte("\r\n"), tc.expected, []byte("\r\n"))))

			// Decoder reads until NNTP ".\r\n"
			encoded.WriteString(".\r\n")

			// Check that we can decode it back again
			dec := NewDecoder(encoded, WithStatusLineAlreadyRead())
			response, err := dec.Next()
			require.NoError(t, err)
			require.Equal(t, tc.input, response.Data)
			require.Equal(t, int64(len(tc.input)), response.Metadata.PartSize)
			require.Equal(t, crc32.ChecksumIEEE(tc.input), response.Metadata.CRC)
			require.Equal(t, int64(len(tc.input)), response.Metadata.End())
		})
	}
}

func TestEncoder(t *testing.T) {
	raw := make([]byte, 1024*1024)
	_, err := randv2.NewChaCha8([32]byte(bytes.Repeat([]byte{0xBA, 0xAD, 0xF0, 0x0D}, 8))).Read(raw)
	require.NoError(t, err)

	encoded := new(bytes.Buffer)
	w, err := NewEncoder(encoded, Meta{
		FileName:   "filename",
		FileSize:   int64(len(raw)),
		PartSize:   int64(len(raw)),
		PartNumber: 1,
		TotalParts: 1,
	})
	require.NoError(t, err)
	_, err = io.Copy(w, bytes.NewReader(raw))
	require.NoError(t, err)
	err = w.Close()
	require.NoError(t, err)

	require.Equal(t, uint32(0xa623f24e), w.crc)

	// Decoder reads until NNTP ".\r\n"
	encoded.WriteString(".\r\n")

	// Check that we can decode it back again
	dec := NewDecoder(encoded, WithStatusLineAlreadyRead())
	response, err := dec.Next()
	require.NoError(t, err)
	require.Equal(t, raw, response.Data)
}

func BenchmarkEncoder(b *testing.B) {
	raw := make([]byte, 1024*1024)
	_, err := rand.Read(raw)
	require.NoError(b, err)

	r := bytes.NewReader(raw)

	meta := Meta{
		FileName:   "filename",
		FileSize:   int64(len(raw)),
		PartSize:   int64(len(raw)),
		PartNumber: 1,
		TotalParts: 1,
	}

	enc, err := NewEncoder(io.Discard, meta)
	require.NoError(b, err)

	b.ResetTimer()
	for b.Loop() {
		_, err = io.Copy(enc, r)
		require.NoError(b, err)
		err = enc.Close()
		require.NoError(b, err)
		_, err = r.Seek(0, io.SeekStart)
		require.NoError(b, err)
		enc.Reset(io.Discard, meta)
	}
}
