package rapidyenc

import (
	"github.com/stretchr/testify/require"
	"testing"
)

type encoderCase struct {
	name     string
	input    string
	expected string
}

func TestEncoderSimple(t *testing.T) {
	cases := []encoderCase{
		{"NUL", "\x00", "\x2a"},
		{"SPACE", "\x20", "\x4a"},
	}

	encoder := NewEncoder()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := encoder.Encode([]byte(tc.input))
			require.NoError(t, err)
			require.Equal(t, []byte(tc.expected), encoded)
		})
	}
}
