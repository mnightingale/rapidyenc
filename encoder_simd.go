//go:build !cgo && goexperiment.simd

package rapidyenc

func encodeSIMD(
	lineSize int,
	colOffset *int,
	src []byte,
	dest []byte,
	doEnd bool,
	kernel func(lineSize int, colOffset *int, src []byte, dest []byte) (int, int),
) []byte {
	length := len(src)
	if length < 1 {
		return dest[:0]
	}
	if colOffset == nil {
		colOffset = new(int)
	}
	if *colOffset < 0 {
		*colOffset = 0
	}

	if length < 12 {
		return encodeGeneric(lineSize, colOffset, src, dest, doEnd)
	}

	consumed, written := kernel(lineSize, colOffset, src, dest)

	// scalar loop to process remaining bytes
	scalarOut := encodeGeneric(lineSize, colOffset, src[consumed:], dest[written:], doEnd)
	return dest[:written+len(scalarOut)]
}
