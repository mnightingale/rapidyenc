package rapidyenc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeGeneric(t *testing.T) {

	cases := []struct {
		name     string
		raw      string
		state    State
		expected string
	}{
		{"special", "\x2e\x2e\x2e\x2e\x2e\x2e\x0d\x0a\x3d\x6e\x2e\x2e\x2e", StateNone, "\u0004\u0004\u0004\u0004\u0004\u0004\u0004\u0004\u0004\u0004"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := tc.state
			dest := make([]byte, len(tc.raw))
			n, d, _, err := decodeGeneric(dest, []byte(tc.raw), &state)
			require.NoError(t, err)
			require.Equal(t, n, len(tc.raw))
			require.Equal(t, []byte(tc.expected), d)
		})
	}
}
