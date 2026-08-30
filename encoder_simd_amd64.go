//go:build !cgo && goexperiment.simd

package rapidyenc

import (
	"math/bits"
	"simd/archsimd"
)

var (
	encodeIncremental func(lineSize int, colOffset *int, src []byte, dest []byte, doEnd bool) []byte
)

func init() {
	if archsimd.X86.AVX2() {
		encodeIncremental = encodeAVX2
		encoderKernel = "AVX2"
	} else {
		encodeIncremental = encodeGeneric
		encoderKernel = "generic"
	}
}

func encodeAVX2(lineSize int, colOffset *int, src []byte, dest []byte, doEnd bool) []byte {
	return encodeSIMD(lineSize, colOffset, src, dest, doEnd, encodeSIMDAVX2)
}

var (
	encoderLUT                lookupsAVX2
	encoderSpecialLUT         archsimd.Int8x32
	broadcast42, broadcast106 archsimd.Int8x32
	firstCharAdj              archsimd.Int8x32
)

type lookupsAVX2 struct {
	eolLastChar    [256]uint32
	shufExpand     [65536][32]byte  // huge 2MB table
	expandMergemix [33 * 2][32]int8 // not used in AVX3
}

func init() {
	encoderLUT = lookupsAVX2{}

	// fill eolLastChar table
	for n := 0; n < 256; n++ {
		if n == 214+'\t' || n == 214+' ' || n == 214+'\x00' || n == 214+'\n' || n == 214+'\r' || n == '='-42 {
			// escaped: =, char+64, \r, \n (4 bytes); the 0x0a in byte 3 naturally sets bit 27
			encoderLUT.eolLastChar[n] = uint32((((n + 42 + 64) & 0xff) << 8) + 0x0a0d003d)
		} else {
			// not escaped: char, \r, \n (3 bytes)
			encoderLUT.eolLastChar[n] = uint32(((n + 42) & 0xff) + 0x0a0d00)
		}
	}

	// fill shufExpand table
	for i := 0; i < 65536; i++ {
		k := i
		var res [32]byte
		p := 0
		for j := 0; j < 16; j++ {
			if (k & 1) != 0 {
				res[j+p] = 0xff
				p++
			}
			res[j+p] = byte(j)
			k >>= 1
		}
		for ; p < 16; p++ {
			res[16+p] = 0x40 // arbitrary value (top bit cannot be set)
		}
		encoderLUT.shufExpand[i] = res
	}

	// fill expandMergemix table
	for i := 0; i < 33; i++ {
		n := 32 - 1 - i
		if i == 32 {
			n = 32
		}
		for j := 0; j < 32; j++ {
			if n >= j {
				encoderLUT.expandMergemix[i*2][j] = -1 // 0xff as int8
			} else {
				encoderLUT.expandMergemix[i*2][j] = 0
			}
			// C++ formula: '='*(n==j) + 64*(n==j-1) + 42*(n!=j)
			// The terms are additive: when n==j-1, n!=j is also true, giving 64+42=106
			var val int8
			if n == j {
				val = '='
			} else if n == j-1 {
				val = 42 + 64
			} else {
				val = 42
			}
			encoderLUT.expandMergemix[i*2+1][j] = val
		}
	}

	encoderSpecialLUT = archsimd.LoadInt8x32([]int8{
		'\n' - 42, -42, ' ' - 42, '=' - 42, -42, '\r' - 42, -42, -42, '\n' - 42, '\t' - 42, '\x00' - 42, '=' - 42, '.' - 42, '\r' - 42, -42, '\x00' - 42,
		'\n' - 42, -42, ' ' - 42, '=' - 42, -42, '\r' - 42, -42, -42, '\n' - 42, '\t' - 42, '\x00' - 42, '=' - 42, '.' - 42, '\r' - 42, -42, '\x00' - 42,
	})
	broadcast42 = archsimd.BroadcastInt8x32(42)
	broadcast106 = archsimd.BroadcastInt8x32(42 + 64)

	// first byte of a line gets saturated offset to also catch tab/space/dot
	firstCharAdj = archsimd.LoadInt8x32([]int8{88, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
}

// encodeSIMDAVX2 performs the AVX2 SIMD encoding kernel.
// It returns (consumed, written) indicating how many source bytes were
// consumed and how many destination bytes were written.
func encodeSIMDAVX2(lineSize int, colOffset *int, src []byte, dest []byte) (int, int) {
	const vecSize = 32
	const chunk = vecSize * 2 // 64 bytes per iteration
	length := len(src)

	// Need at least chunk+1 bytes for the SIMD loop to operate safely.
	// The extra 1 is for the fast-path shifted load (1 byte before current position).
	if length <= chunk+1 || lineSize < 16 {
		return 0, 0
	}

	pos := 0 // read position in src
	wp := 0  // write position in dest
	lineSizeOffset := -lineSize + 1
	col := *colOffset + lineSizeOffset

	// Pre-loop: always process at least one byte to ensure the fast-path
	// shifted load at pos-1 is valid on the first iteration.
	if col < 0 && col != lineSizeOffset {
		// not the first/last character of a line
		c := src[pos]
		pos++
		if c == 214 || c == '\n'+214 || c == '\r'+214 || c == '='-42 {
			dest[wp] = '='
			dest[wp+1] = c + 42 + 64
			wp += 2
			col += 2
		} else {
			dest[wp] = c + 42
			wp++
			col++
		}
	}

	// Handle EOL at the very start (colOffset was near line end)
	if col >= 0 {
		c := src[pos]
		pos++
		if col == 0 {
			// last char of line
			eolChar := encoderLUT.eolLastChar[c]
			_ = dest[wp+3]
			dest[wp] = byte(eolChar)
			dest[wp+1] = byte(eolChar >> 8)
			dest[wp+2] = byte(eolChar >> 16)
			dest[wp+3] = byte(eolChar >> 24)
			wp += 3 + int(eolChar>>27)
			col = lineSizeOffset
		} else {
			// line overflowed, insert a newline
			if escaped := escapedLUT[c]; escaped != 0 {
				_ = dest[wp+3]
				dest[wp] = '\r'
				dest[wp+1] = '\n'
				dest[wp+2] = byte(escaped)
				dest[wp+3] = byte(escaped >> 8)
				wp += 4
				col = 2 - lineSize + 1
			} else {
				_ = dest[wp+2]
				dest[wp] = '\r'
				dest[wp+1] = '\n'
				dest[wp+2] = c + 42
				wp += 3
				col = 2 - lineSize
			}
		}
	}

	// Handle first char of line (needs special escaping for tab, space, dot)
	if col == lineSizeOffset {
		c := src[pos]
		pos++
		if escaped := escapedLUT[c]; escaped != 0 {
			dest[wp] = byte(escaped)
			dest[wp+1] = byte(escaped >> 8)
			wp += 2
			col += 2
		} else {
			dest[wp] = c + 42
			wp++
			col++
		}
	}

	// Main SIMD loop
	// endPos: the SIMD loop needs room for the current chunk (64) plus
	// a possible EOL reload (65 more bytes), so we stop at length - chunk - 1.
	endPos := length - chunk - 1

	var cmpA, cmpB archsimd.Mask8x32
	var dataA, dataB archsimd.Int8x32
	var maskA, maskB uint32
	var maskBitsA, maskBitsB int
	var bitIndexA, bitIndexB int
	var outputBytesA int
	var readPos int
	var zeroI8 archsimd.Int8x32 // zero vector for sign-bit comparisons

	for pos+chunk <= endPos {
		readPos = pos
		dataA = archsimd.LoadUint8x32(src[readPos:]).AsInt8x32()
		dataB = archsimd.LoadUint8x32(src[readPos+vecSize:]).AsInt8x32()
		pos += chunk

		// search for special chars
		cmpA = dataA.Equal(encoderSpecialLUT.PermuteOrZeroGrouped(dataA.Abs()))
		cmpB = dataB.Equal(encoderSpecialLUT.PermuteOrZeroGrouped(dataB.Abs()))

	processMasks:
		readPos = pos - chunk // recompute for current batch (needed after EOL reload)
		maskA = cmpA.ToBits()
		maskB = cmpB.ToBits()
		maskBitsA = bits.OnesCount32(maskA)
		maskBitsB = bits.OnesCount32(maskB)
		outputBytesA = maskBitsA + vecSize

		if maskBitsA|maskBitsB > 1 {
			// slow path: multiple escape characters
			m1 := maskA & 0xffff
			m2 := (maskA >> 11) & 0x1fffe0
			m3 := maskB & 0xffff
			m4 := (maskB >> 11) & 0x1fffe0
			var shuf1A, shuf2A, shuf1B, shuf2B archsimd.Int8x32
			var data1A, data2A, data1B, data2B archsimd.Uint8x32

			// add +42 (or +106 for escaped chars)
			dataA = dataA.Add(broadcast106.Merge(broadcast42, cmpA))
			dataB = dataB.Add(broadcast106.Merge(broadcast42, cmpB))

			// duplicate halves: data1 = both lanes contain low half, data2 = both contain high half
			data1A = dataA.ConcatPermute128Scalars(0, 0, dataA).AsUint8x32()
			data1B = dataB.ConcatPermute128Scalars(0, 0, dataB).AsUint8x32()
			data2A = dataA.ConcatPermute128Scalars(1, 1, dataA).AsUint8x32()
			data2B = dataB.ConcatPermute128Scalars(1, 1, dataB).AsUint8x32()

			shuf1A = archsimd.LoadUint8x32Array(&encoderLUT.shufExpand[m1]).AsInt8x32()
			shuf2A = archsimd.LoadUint8x32Array(&encoderLUT.shufExpand[m2>>5]).AsInt8x32()
			shuf1B = archsimd.LoadUint8x32Array(&encoderLUT.shufExpand[m3]).AsInt8x32()
			shuf2B = archsimd.LoadUint8x32Array(&encoderLUT.shufExpand[m4>>5]).AsInt8x32()

			// sign-bit masks: Less(zero) gives TRUE where byte < 0 (high bit set)
			signMask1A := shuf1A.Less(zeroI8)
			signMask2A := shuf2A.Less(zeroI8)
			signMask1B := shuf1B.Less(zeroI8)
			signMask2B := shuf2B.Less(zeroI8)

			// expand
			data1A = data1A.PermuteOrZeroGrouped(shuf1A)
			data2A = data2A.PermuteOrZeroGrouped(shuf2A)
			data1B = data1B.PermuteOrZeroGrouped(shuf1B)
			data2B = data2B.PermuteOrZeroGrouped(shuf2B)

			// add in '=' where shuf has high bit set (escape marker positions)
			data1A = broadcastEQ.AsUint8x32().Merge(data1A, signMask1A)
			data2A = broadcastEQ.AsUint8x32().Merge(data2A, signMask2A)
			data1B = broadcastEQ.AsUint8x32().Merge(data1B, signMask1B)
			data2B = broadcastEQ.AsUint8x32().Merge(data2B, signMask2B)

			shuf1Len := bits.OnesCount32(m1) + 16
			shuf3Len := bits.OnesCount32(m3) + 16
			data1A.Store(dest[wp:])
			data2A.Store(dest[wp+shuf1Len:])
			data1B.Store(dest[wp+outputBytesA:])
			data2B.Store(dest[wp+outputBytesA+shuf3Len:])
			outputBytes := vecSize + outputBytesA + maskBitsB
			wp += outputBytes
			col += outputBytes

			if col >= 0 {
				// we overflowed - find correct position to revert back to
				var eqMask uint64
				shiftAmt := maskBitsB + vecSize - 1 - col
				if shiftAmt < 0 {
					eqMask = uint64(signMask1A.ToBits()) | (uint64(signMask2A.ToBits()) << shuf1Len)
					pos += maskBitsB
					shiftAmt += outputBytesA
				} else {
					eqMask = uint64(signMask1B.ToBits()) | (uint64(signMask2B.ToBits()) << shuf3Len)
				}

				eqMask >>= shiftAmt
				bitCount := bits.OnesCount64(eqMask)
				pos += bitCount
				revert := col + int(eqMask&1)
				wp -= revert
				pos -= revert

				// EOL handling + reload
				var loaded bool
				pos, col, wp, dataA, dataB, cmpA, cmpB, loaded = encodeEOLHandle(src, pos, col, wp, dest, lineSizeOffset)
				if !loaded {
					break
				}
				goto processMasks
			}
			continue
		}

		// fast path: at most 1 escape character per vector
		maskBitsB += vecSize

		bitIndexA = bits.LeadingZeros32(maskA)
		bitIndexB = bits.LeadingZeros32(maskB)

		mergeMaskA := archsimd.LoadInt8x32Array(&encoderLUT.expandMergemix[bitIndexA*2])
		mergeMaskB := archsimd.LoadInt8x32Array(&encoderLUT.expandMergemix[bitIndexB*2])

		// load shifted data (data at position -1, for insertion of '=' before the escaped byte)
		dataAShifted := archsimd.LoadUint8x32(src[readPos-1:]).AsInt8x32()
		dataBShifted := archsimd.LoadUint8x32(src[readPos+vecSize-1:]).AsInt8x32()

		// clear space for '=' char: dataA = dataA & ~cmpA
		dataA = dataA.AndNot(cmpA.ToInt8x32())
		// blend shifted and original data based on merge mask
		dataA = dataA.Merge(dataAShifted, mergeMaskA.ToMask())
		// add offset (42, or '=' + 64 at escape position)
		addA := archsimd.LoadInt8x32Array(&encoderLUT.expandMergemix[bitIndexA*2+1])
		dataA = dataA.Add(addA)
		dataA.AsUint8x32().Store(dest[wp:])

		// handle the extra byte that spills past the 32-byte store
		dest[wp+vecSize] = src[pos-1-vecSize] + 42 + byte(64)&byte(maskA>>(vecSize-1-6))
		wp += outputBytesA

		// same for dataB
		dataB = dataB.AndNot(cmpB.ToInt8x32())
		dataB = dataB.Merge(dataBShifted, mergeMaskB.ToMask())
		dataB = dataB.Add(archsimd.LoadInt8x32Array(&encoderLUT.expandMergemix[bitIndexB*2+1]))
		dataB.AsUint8x32().Store(dest[wp:])

		dest[wp+vecSize] = src[pos-1] + 42 + byte(64)&byte(maskB>>(vecSize-1-6))
		wp += maskBitsB

		col += outputBytesA + maskBitsB

		if col >= 0 {
			// EOL handling for fast path
			if col > maskBitsB {
				bitIndexA += 1 + maskBitsB
				pos += maskBitsB - vecSize
				if col == bitIndexA {
					// this is an escape character, so line will need to overflow
					wp--
				} else if col > bitIndexA {
					pos++
				}
			} else {
				bitIndexB++
				if col == bitIndexB {
					wp--
				} else if col > bitIndexB {
					pos++
				}
			}
			pos -= col
			wp -= col

			var loaded bool
			pos, col, wp, dataA, dataB, cmpA, cmpB, loaded = encodeEOLHandle(src, pos, col, wp, dest, lineSizeOffset)
			if !loaded {
				break
			}
			goto processMasks
		}
	}

	*colOffset = col + lineSize - 1
	return pos, wp
}

// encodeEOLHandle handles end-of-line processing and prepares next iteration.
// It writes the EOL character and reloads the next two vectors.
func encodeEOLHandle(
	src []byte, pos, col, wp int, dest []byte, lineSizeOffset int,
) (int, int, int, archsimd.Int8x32, archsimd.Int8x32, archsimd.Mask8x32, archsimd.Mask8x32, bool) {
	const vecSize = 32
	const chunk = vecSize * 2

	// write EOL character
	eolChar := encoderLUT.eolLastChar[src[pos]]
	_ = dest[wp+3]
	dest[wp] = byte(eolChar)
	dest[wp+1] = byte(eolChar >> 8)
	dest[wp+2] = byte(eolChar >> 16)
	dest[wp+3] = byte(eolChar >> 24)
	wp += 3 + int(eolChar>>27)
	col = lineSizeOffset

	// Check if we've exhausted the SIMD-processable range
	endPos := len(src) - chunk - 1
	if pos >= endPos {
		pos++
		var zeroVec archsimd.Int8x32
		var zeroMask archsimd.Mask8x32
		return pos, col, wp, zeroVec, zeroVec, zeroMask, zeroMask, false
	}

	// load next vectors for the new line (data starts at pos+1, after the EOL char)
	dataA := archsimd.LoadUint8x32(src[pos+1:]).AsInt8x32()
	dataB := archsimd.LoadUint8x32(src[pos+1+vecSize:]).AsInt8x32()
	pos += chunk + 1

	// search for special chars (with first-char adjustment)
	// The first byte of a line also needs to catch tab, space, and dot,
	// so we add 88 to the first 8 bytes to saturate those indices in the LUT
	cmpA := dataA.Equal(encoderSpecialLUT.PermuteOrZeroGrouped(dataA.Abs().AddSaturated(firstCharAdj)))
	cmpB := dataB.Equal(encoderSpecialLUT.PermuteOrZeroGrouped(dataB.Abs()))

	return pos, col, wp, dataA, dataB, cmpA, cmpB, true
}
