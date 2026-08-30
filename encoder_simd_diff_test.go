//go:build !cgo && goexperiment.simd && (amd64 || arm64)

package rapidyenc

import (
	"bytes"
	"math/rand"
	"testing"
)

// encodeGeneric is the reference the SIMD encoders accelerate: for the same
// input, line size and starting column they must produce identical output and
// leave the column in the same place.

func encodeBoth(t *testing.T, lineSize, startCol int, src []byte, doEnd bool) (out, want []byte, col, wantCol int) {
	t.Helper()
	dest := make([]byte, maxLength(len(src), lineSize))
	col = startCol
	out = append([]byte(nil), encodeIncremental(lineSize, &col, src, dest, doEnd)...)

	refDest := make([]byte, maxLength(len(src), lineSize))
	wantCol = startCol
	want = append([]byte(nil), encodeGeneric(lineSize, &wantCol, src, refDest, doEnd)...)
	return
}

func checkEncode(t *testing.T, lineSize, startCol int, src []byte, doEnd bool) bool {
	t.Helper()
	got, want, col, wantCol := encodeBoth(t, lineSize, startCol, src, doEnd)
	if bytes.Equal(got, want) && col == wantCol {
		return true
	}
	diff := 0
	for diff < len(got) && diff < len(want) && got[diff] == want[diff] {
		diff++
	}
	t.Errorf("mismatch (lineSize=%d startCol=%d len=%d doEnd=%v), first differing byte at %d\n src=%q\n generic: col=%d len=%d out=%q\n simd:    col=%d len=%d out=%q",
		lineSize, startCol, len(src), doEnd, diff, src, wantCol, len(want), want, col, len(got), got)
	return false
}

func TestEncodeSIMDMatchesGeneric(t *testing.T) {
	rnd := rand.New(rand.NewSource(1))
	// bytes that need escaping once offset by 42, plus the line-start specials
	specials := []byte{214, 214 + '\r', 214 + '\n', '=' - 42, 214 + '\t', 214 + ' ', '.' - 42}

	for _, lineSize := range []int{64, 65, 100, 128, 200, 256} {
		for _, size := range []int{64, 65, 96, 128, 129, 200, 512, 1000, 4096} {
			for iter := 0; iter < 200; iter++ {
				src := make([]byte, size)
				for i := range src {
					switch rnd.Intn(4) {
					case 0:
						src[i] = specials[rnd.Intn(len(specials))]
					default:
						src[i] = byte(rnd.Intn(256))
					}
				}
				if !checkEncode(t, lineSize, rnd.Intn(lineSize), src, rnd.Intn(2) == 0) {
					t.Fatalf("stopping after first mismatch (size=%d iter=%d)", size, iter)
				}
			}
		}
	}
}

// TestEncodeSIMDEscapeRuns walks escape sequences past the vector and line
// boundaries, where the kernels have to rewind and re-emit.
func TestEncodeSIMDEscapeRuns(t *testing.T) {
	specials := []byte{214, 214 + '\r', 214 + '\n', '=' - 42, 214 + '\t', 214 + ' ', '.' - 42}
	for _, runLen := range []int{1, 2, 3, 8, 16, 17, 33} {
		for _, sp := range specials {
			for _, lineSize := range []int{64, 128} {
				for pos := 0; pos < 200; pos++ {
					src := bytes.Repeat([]byte{'A' - 42}, 300)
					for i := pos; i < pos+runLen && i < len(src); i++ {
						src[i] = sp
					}
					for _, startCol := range []int{0, 1, lineSize - 2, lineSize - 1} {
						if !checkEncode(t, lineSize, startCol, src, false) {
							t.Fatalf("stopping: runLen=%d special=%d pos=%d", runLen, sp, pos)
						}
					}
				}
			}
		}
	}
}
