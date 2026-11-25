package rapidyenc

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A body line that starts with '`' or ' ' has a uu length of 0, so
// detectFormat sliced line[1:0] and panicked before the format was known.
func TestDetectFormatZeroLengthUULine(t *testing.T) {
	for _, first := range []string{"`abc", " abc", "``", "  "} {
		t.Run(first, func(t *testing.T) {
			body := "222 Body follows\r\n" + first + "\r\nhello\r\n.\r\n"
			require.NotPanics(t, func() {
				_, _ = NewDecoder(strings.NewReader(body)).Next()
			})
		})
	}
}
