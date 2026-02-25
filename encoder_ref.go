//go:build !cgo && !amd64

package rapidyenc

func encodeIncremental(lineLength int, column *int, src []byte, dest []byte, isEnd bool) []byte {
	if column == nil {
		column = new(int)
	}

	return encodeGeneric(lineLength, column, src, dest, isEnd)
}
