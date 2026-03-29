//go:build !cgo && !(goexperiment.simd && amd64)

package rapidyenc

func decodeIncremental(dst, src []byte, state State) (int, []byte, State, End, error) {
	return decodeGeneric(dst, src, state)
}
