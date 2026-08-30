package rapidyenc

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// Response.Data is handed to the caller and keeps its capacity for the lifetime
// of the response, so anything allocated beyond the decoded length is overhead
// the caller cannot reclaim. It is sized once, from the yEnc headers.
func wantCap(partSize int) int {
	return max(partSize+64, yencMinBufferSize)
}

func TestDecodeNoExcessAllocation(t *testing.T) {
	for _, size := range []int{1, 100, 1000, 5000, 100_000, 768_000} {
		raw := bytes.Repeat([]byte("abcdefgh"), size/8+1)[:size]

		encoded, err := body(raw)
		require.NoError(t, err)

		dec := NewDecoder(encoded, WithStatusLineAlreadyRead())
		response, err := dec.Next()
		require.NoError(t, err)
		require.Equal(t, raw, response.Data)
		require.Equal(t, wantCap(size), cap(response.Data), "size=%d was grown or over-allocated", size)
	}
}

// Input regularly spans several responses, so the tail of one sits in the same
// read buffer as the whole of the next. That trailing data must not drive this
// response's buffer size.
func TestDecodeNoExcessAllocationMultipleResponses(t *testing.T) {
	sizes := []int{1000, 50_000, 300, 120_000}

	for _, bufSize := range []int{1024, 8 * 1024, 64 * 1024, 512 * 1024} {
		stream := new(bytes.Buffer)
		raws := make([][]byte, len(sizes))
		for i, size := range sizes {
			raws[i] = bytes.Repeat([]byte("abcdefgh"), size/8+1)[:size]
			encoded, err := body(raws[i])
			require.NoError(t, err)
			_, err = io.Copy(stream, encoded)
			require.NoError(t, err)
		}

		dec := NewDecoder(stream, WithStatusLineAlreadyRead(), WithBufferSize(bufSize))
		for i, size := range sizes {
			response, err := dec.Next()
			require.NoError(t, err, "bufSize=%d response=%d", bufSize, i)
			require.Equal(t, raws[i], response.Data, "bufSize=%d response=%d", bufSize, i)
			require.Equal(t, wantCap(size), cap(response.Data),
				"bufSize=%d response=%d was grown or over-allocated", bufSize, i)
		}
	}
}
