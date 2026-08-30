//go:build !cgo && goexperiment.simd && (amd64 || arm64)

package rapidyenc

import (
	"bytes"
	"math/rand"
	"testing"
)

// The SIMD kernels are only exercised above a minimum length, and which bytes
// land in a vector block depends on the source alignment, so these tests sweep
// both. decodeGeneric is the reference: every SIMD path is an optimisation of
// it and must agree byte for byte, including the returned state and end.

// yencAlphabet is biased towards the bytes that drive the special-case paths:
// escapes, line endings, dot-stuffing and the "=y" terminator.
var yencAlphabet = []byte("=\r\n.y\r\n=abcABC \x00\xff*")

var decodeStates = []State{StateNone, StateCRLF, StateCR, StateEQ, StateCRLFDT, StateCRLFDTCR, StateCRLFEQ}

// place copies src into a buffer at the given offset, with slack after it so
// the kernels' read-ahead (up to 16 bytes past the last block) stays in-bounds.
func place(src []byte, offset int) []byte {
	buf := make([]byte, offset+len(src)+128)
	copy(buf[offset:], src)
	return buf[offset : offset+len(src)]
}

func randYenc(rnd *rand.Rand, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		if rnd.Intn(4) == 0 {
			out[i] = yencAlphabet[rnd.Intn(len(yencAlphabet))]
		} else {
			out[i] = byte(rnd.Intn(256))
		}
	}
	return out
}

// injectSequences sprinkles the multi-byte sequences the kernels search for.
func injectSequences(rnd *rand.Rand, src []byte) {
	seqs := [][]byte{
		[]byte("\r\n."), []byte("\r\n.\r\n"), []byte("\r\n=y"), []byte("\r\n.=y"),
		[]byte("=y"), []byte("===="), []byte("=\r\n"), []byte("=="), []byte("\r\n..\r\n"),
	}
	for i := 0; i < len(src)/32; i++ {
		copy(src[rnd.Intn(len(src)):], seqs[rnd.Intn(len(seqs))])
	}
}

type decodeResult struct {
	n     int
	out   []byte
	state State
	end   End
}

func (r decodeResult) equal(o decodeResult) bool {
	return r.n == o.n && r.state == o.state && r.end == o.end && bytes.Equal(r.out, o.out)
}

// decodeAll drives an incremental decoder to completion.
func decodeAll(t *testing.T, fn func(dst, src []byte, state State) (int, []byte, State, End, error), src []byte, offset int, state State) decodeResult {
	t.Helper()
	res := decodeResult{state: state}
	for {
		dst := make([]byte, len(src)-res.n+64)
		n, dec, st, end, err := fn(dst, place(src[res.n:], offset), res.state)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		res.out = append(res.out, dec...)
		res.n += n
		res.state = st
		if end != EndNone || n == 0 || res.n >= len(src) {
			res.end = end
			return res
		}
	}
}

func checkDecode(t *testing.T, src []byte, offset int, state State) bool {
	t.Helper()
	want := decodeAll(t, decodeGeneric, src, offset, state)
	got := decodeAll(t, decodeIncremental, src, offset, state)
	if want.equal(got) {
		return true
	}
	diff := 0
	for diff < len(want.out) && diff < len(got.out) && want.out[diff] == got.out[diff] {
		diff++
	}
	t.Errorf("mismatch (offset=%d state=%v), first differing output byte at %d\n src=%q\n generic: n=%d state=%v end=%v out(%d)=%q\n simd:    n=%d state=%v end=%v out(%d)=%q",
		offset, state, diff, src,
		want.n, want.state, want.end, len(want.out), want.out,
		got.n, got.state, got.end, len(got.out), got.out)
	return false
}

func TestDecodeSIMDMatchesGeneric(t *testing.T) {
	rnd := rand.New(rand.NewSource(1))
	for _, size := range []int{129, 130, 135, 150, 200, 256, 300, 512, 1000, 4096} {
		for iter := 0; iter < 500; iter++ {
			src := randYenc(rnd, size)
			injectSequences(rnd, src)
			if !checkDecode(t, src, rnd.Intn(64), decodeStates[rnd.Intn(len(decodeStates))]) {
				t.Fatalf("stopping after first mismatch at size=%d iter=%d", size, iter)
			}
		}
	}
}

// TestDecodeSIMDBoundaries walks each interesting sequence past every vector
// block boundary, at every source alignment, since the kernels stitch blocks
// together with lookahead and carry state between them.
func TestDecodeSIMDBoundaries(t *testing.T) {
	seqs := []string{
		"\r\n.", "\r\n.\r\n", "\r\n=y", "\r\n.=y", "\r\n..", "=y", "=\r\n", "====", "==", "=",
		"\r\n.=A", "\r\n.\r", "\r\n.\n", "\r\n\r\n.\r\n",
	}
	for _, seq := range seqs {
		for _, state := range []State{StateNone, StateCRLF, StateCR} {
			for pos := 0; pos < 140; pos++ {
				src := bytes.Repeat([]byte("A"), 200)
				copy(src[pos:], seq)
				for offset := 0; offset < 64; offset += 7 {
					if !checkDecode(t, src, offset, state) {
						t.Fatalf("stopping: seq=%q pos=%d", seq, pos)
					}
				}
			}
		}
	}
}
