//go:build !cgo && !(goexperiment.simd && (amd64 || arm64))

package rapidyenc

func encodeIncremental(lineLength int, column *int, src []byte, dest []byte, isEnd bool) []byte {
	return encodeGeneric(lineLength, column, src, dest, isEnd)
}
