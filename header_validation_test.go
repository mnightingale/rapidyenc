package rapidyenc

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// =ypart begin is 1-based, so a value below 1 would place Offset before the
// start of the file. Callers use Offset to position the part in the output.
func TestYpartBounds(t *testing.T) {
	cases := []struct {
		line     string
		offset   int64
		partSize int64
	}{
		{"=ypart begin=1 end=100", 0, 100},
		{"=ypart begin=501 end=1000", 500, 500},
		{"=ypart begin=0 end=100", 0, 100},
		{"=ypart begin=-5 end=100", 0, 100},
		{"=ypart end=100", 0, 100},
		{"=ypart begin=100 end=1", 99, 0},
	}

	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			r := &Response{}
			r.processYencHeader([]byte(tc.line))
			require.GreaterOrEqual(t, r.Metadata.Offset, int64(0), "offset must never be negative")
			require.Equal(t, tc.offset, r.Metadata.Offset)
			require.Equal(t, tc.partSize, r.Metadata.PartSize)
		})
	}
}

// A header size close to MaxInt64 must clamp rather than overflow the margin.
func TestExpectedSizeClamped(t *testing.T) {
	cases := []struct {
		partSize int64
		fileSize int64
		want     int
	}{
		{100, 0, yencMinBufferSize},
		{0, 0, yencMinBufferSize},
		{-1, -1, yencMinBufferSize},
		{1 << 20, 0, 1<<20 + 64},
		{1 << 40, 0, yencMaxInitialAlloc},
		{math.MaxInt64, 0, yencMaxInitialAlloc},
		{0, math.MaxInt64, yencMaxInitialAlloc},
	}

	for _, tc := range cases {
		r := &Response{}
		r.Metadata.PartSize = tc.partSize
		r.Metadata.FileSize = tc.fileSize
		require.Equal(t, tc.want, r.computeExpectedSize(),
			"partSize=%d fileSize=%d", tc.partSize, tc.fileSize)
	}
}
