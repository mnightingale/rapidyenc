//go:build !cgo && !goexperiment.simd

package rapidyenc

func decodeIncremental(dst, src []byte, state *State) (int, []byte, End, error) {
	if state == nil {
		state = new(StateCRLF)
	}

	return decodeGeneric(dst, src, state)
}
