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

	if consumed >= length {
		// no tail for encodeGeneric, so apply its trailing whitespace escape here
		if doEnd && written > 0 {
			if lc := dest[written-1]; lc == '\t' || lc == ' ' {
				dest[written-1] = '='
				dest[written] = lc + 64
				written++
				*colOffset = *colOffset + 1
			}
		}
		return dest[:written]
	}

	// scalar loop to process remaining bytes
	scalarOut := encodeGeneric(lineSize, colOffset, src[consumed:], dest[written:], doEnd)
	return dest[:written+len(scalarOut)]
}
