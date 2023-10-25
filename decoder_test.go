package rapidyenc

import (
	"crypto/rand"
	"github.com/stretchr/testify/require"
	"testing"
)

type decoderCase struct {
	name     string
	input    string
	expected string
}

func TestDecoderSimple(t *testing.T) {
	cases := []decoderCase{
		{"SPACE", "\x4a", "\x20"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dst := make([]byte, len(tc.input)*2)
			src := []byte(tc.input)
			state := StateCRLF

			n, d, _ := DecodeIncremental(dst, src, &state)
			require.Equal(t, len(tc.input), n)
			require.Equal(t, len(tc.expected), d)
			require.Equal(t, tc.expected, string(dst[:d]))
		})
	}
}

func TestEncoderAndDecoder(t *testing.T) {
	raw := make([]byte, 128000)
	_, err := rand.Read(raw)
	require.NoError(t, err)
	enc := NewEncoder()
	encoded, _ := enc.Encode(raw)

	decoded := make([]byte, len(encoded))

	state := StateCRLF
	nsrc, ndst, _ := DecodeIncremental(decoded, encoded, &state)
	require.Equal(t, len(encoded), nsrc)
	require.Equal(t, len(raw), ndst)
	require.Equal(t, raw, decoded[:ndst])
}
