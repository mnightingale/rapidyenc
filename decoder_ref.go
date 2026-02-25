//go:build !cgo && !amd64

package rapidyenc

func decodeIncremental(dst, src []byte, state *State) (int, []byte, End, error) {
	if state == nil {
		state = new(StateCRLF)
	}

	return decodeGeneric(dst, src, state)
}
