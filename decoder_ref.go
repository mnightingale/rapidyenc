//go:build !cgo && !amd64 && !arm64

package rapidyenc

func decodeIncremental(dst, src []byte, state *State) (int, []byte, End, error) {
	if state == nil {
		unusedState := StateCRLF
		state = &unusedState
	}

	return decodeGeneric(dst, src, state)
}
