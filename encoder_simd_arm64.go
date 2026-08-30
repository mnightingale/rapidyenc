//go:build !cgo && goexperiment.simd

package rapidyenc

import (
	"encoding/binary"
	"math/bits"
	"simd/archsimd"
)

// Port of the aarch64 paths in encoder_neon.cc, using forward positions rather
// than the reference's negative index. col keeps the reference's bias, where 0
// means the next byte is the last on the line, and is un-biased on the way out.

const (
	encVecSize = 16
	// extra chars for EOL handling, -1 so the loop bound is a strict <
	encInputOffset = encVecSize*4 - 1
)

var (
	// TBL indices expanding 8 input bytes to up to 16 output bytes; escape and
	// filler positions index out of range, so encShufEqLUT supplies the '='
	encShufLUT   [256][16]byte
	encShufEqLUT [256][16]byte
	// marks which output bytes are a '='
	encExpandLUT [256]uint16

	encSpecialLUT               archsimd.Uint8x16
	encEolLutLo, encEolLutHi    archsimd.Uint8x16
	encEolIndexBias             archsimd.Int8x16
	encBlendPosA, encBlendPosB  archsimd.Uint8x16
	broadcastEqM42, broadcast64 archsimd.Uint8x16
	broadcast16, broadcast32    archsimd.Uint8x16
	encClearFirstBit            archsimd.Uint8x16
)

func init() {
	for i := range encShufLUT {
		k := i
		expand := uint16(0)
		var res, eq [16]byte
		p := 0
		for j := 0; j < 8; j++ {
			if k&1 != 0 {
				res[j+p] = '=' // out of TBL range, so the lookup yields 0
				eq[j+p] = '='
				expand |= 1 << (j + p)
				p++
			}
			res[j+p] = byte(j)
			k >>= 1
		}
		for ; p < 8; p++ {
			res[8+p] = byte(8 + p + 0x80) // discarded; out of TBL range
		}
		encShufLUT[i] = res
		encShufEqLUT[i] = eq
		encExpandLUT[i] = expand
	}

	//                                        \0                     \n        \r
	encSpecialLUT = archsimd.LoadUint8x16([]byte{255, 0, 0, 0, 0, 0, 0, 0, 0, 0, 255, 0, 0, 255, 0, 0})

	// The first two bytes of a line escape more characters than the rest. The
	// biased halving add below gives each lane an index that only reaches the
	// characters it cares about; out of range falls back to the '='-42 background.
	// off(c) is the encoded character c as it appears in the input
	off := func(c byte) byte { return c - 42 }
	const none = 0x80 // never matches an input byte at these indices
	encEolLutLo = archsimd.LoadUint8x16([]byte{
		off(0), none, none, off(0), off('\t'), off('\n'), off('\r'), off('\t'),
		off('\n'), off('\r'), none, none, off(0), none, none, none,
	})
	encEolLutHi = archsimd.LoadUint8x16([]byte{
		off(' '), off('\n'), off('\r'), off(' '), none, none, none, none,
		none, none, off('.'), none, none, none, off('='), none,
	})
	// the reference uses vhaddq_s8; archsimd only has the rounding halving add,
	// so bias by one less to get the same result
	encEolIndexBias = archsimd.LoadInt8x16([]int8{
		41, 47, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65,
	})

	// each lane's bit position in the 64-bit mask, as a leading-zero count
	encBlendPosA = archsimd.LoadUint8x16([]byte{63, 62, 61, 60, 51, 50, 49, 48, 47, 46, 45, 44, 35, 34, 33, 32})
	encBlendPosB = archsimd.LoadUint8x16([]byte{31, 30, 29, 28, 19, 18, 17, 16, 15, 14, 13, 12, 3, 2, 1, 0})

	broadcastEqM42 = archsimd.BroadcastUint8x16('=' - 42)
	broadcast64 = archsimd.BroadcastUint8x16(64)
	broadcast16 = archsimd.BroadcastUint8x16(16)
	broadcast32 = archsimd.BroadcastUint8x16(32)
	encClearFirstBit = archsimd.LoadUint8x16([]byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})

	encoderKernel = "NEON"
}

func encodeIncremental(lineSize int, colOffset *int, src []byte, dest []byte, doEnd bool) []byte {
	return encodeSIMD(lineSize, colOffset, src, dest, doEnd, encodeNEON)
}

// encSpecialChars marks \0, \n, \r and = in data, which is already offset by
// 42. Same TBX substitution as the decoder's specialChars.
func encSpecialChars(cmpEq, data archsimd.Uint8x16) archsimd.Uint8x16 {
	return encSpecialLUT.LookupOrZero(data).Or(cmpEq)
}

// encExpand applies one shuffle-LUT entry, inserting '=' before escaped bytes.
func encExpand(data archsimd.Uint8x16, m uint64) archsimd.Uint8x16 {
	return data.LookupOrZero(archsimd.LoadUint8x16Array(&encShufLUT[m])).
		Or(archsimd.LoadUint8x16Array(&encShufEqLUT[m]))
}

// encMask packs the comparisons into the reference's 64-bit mask layout:
// bytes 0..3 hold four bits each of A, bytes 4..7 the same for B
func encMask(cmpA, cmpB archsimd.Uint8x16) (archsimd.Uint8x16, uint64) {
	cmpMerge := addPairs(cmpA.And(permuteBitMask), cmpB.And(permuteBitMask))
	cmpMerge = addPairs(cmpMerge, cmpMerge)
	return cmpMerge, cmpMerge.ReshapeToUint64s().GetElem(0)
}

// encCounts returns the four per-group output lengths packed into a uint32;
// the low half of the 128-bit ADDP holds the same bytes as the reference's VPADD
func encCounts(cmpMerge archsimd.Uint8x16, base uint32) uint32 {
	packed := addPairs(cmpMerge, cmpMerge)
	return packed.OnesCount().ReshapeToUint32s().GetElem(0) + base
}

func encodeNEON(lineSize int, colOffset *int, src []byte, dest []byte) (int, int) {
	length := len(src)
	if length <= encInputOffset || lineSize < encVecSize*4 {
		return 0, 0
	}

	p := 0
	pos := 0
	end := length - encInputOffset
	lineSizeOffset := -lineSize + 32
	col := *colOffset - lineSize + 1

	if col == -lineSize+1 {
		c := src[pos]
		pos++
		if e := escapedLUT[c]; e != 0 {
			binary.LittleEndian.PutUint16(dest[p:], e)
			p += 2
			col += 2
		} else {
			dest[p] = c + 42
			p++
			col++
		}
	}
	if col >= 0 {
		if col == 0 {
			pos, p, col = encodeEOLHandlePre(src, pos, dest, p, lineSizeOffset)
		} else {
			c := src[pos]
			pos++
			dest[p] = '\r'
			dest[p+1] = '\n'
			if e := escapedLUT[c]; e != 0 {
				binary.LittleEndian.PutUint16(dest[p+2:], e)
				p += 4
				col = 2 - lineSize + 1
			} else {
				dest[p+2] = c + 42
				p += 3
				col = 2 - lineSize
			}
		}
	}

	for pos < end {
		dataA := archsimd.LoadUint8x16(src[pos:])
		dataB := archsimd.LoadUint8x16(src[pos+encVecSize:])
		pos += encVecSize * 2

		// search for special chars
		cmpEqA := dataA.Equal(broadcastEqM42).ToInt8x16().ToBits()
		cmpEqB := dataB.Equal(broadcastEqM42).ToInt8x16().ToBits()
		dataA = dataA.Add(broadcast42)
		dataB = dataB.Add(broadcast42)
		cmpA := encSpecialChars(cmpEqA, dataA)
		cmpB := encSpecialChars(cmpEqB, dataB)

		// escaped chars are written as '=' followed by char+64
		dataA = dataA.Or(cmpA.And(broadcast64))
		dataB = dataB.Or(cmpB.And(broadcast64))

		cmpMerge, mask := encMask(cmpA, cmpB)

		if mask&(mask-1) != 0 {
			// more than one escape in this chunk: expand via the shuffle LUT
			mask |= mask >> 8
			m1 := mask & 0xff
			m2 := (mask >> 16) & 0xff
			m3 := (mask >> 32) & 0xff
			m4 := (mask >> 48) & 0xff

			data1A := encExpand(dataA, m1)
			data2A := encExpand(dataA.ConcatShiftBytesRight(dataA, 8), m2)
			data1B := encExpand(dataB, m3)
			data2B := encExpand(dataB.ConcatShiftBytesRight(dataB, 8), m4)

			counts := encCounts(cmpMerge, 0x08080808)
			shuf1Len := int(counts & 0xff)
			shuf2Len := int((counts >> 8) & 0xff)
			shuf3Len := int((counts >> 16) & 0xff)
			shuf4Len := int((counts >> 24) & 0xff)
			shufTotalLen := int((counts * 0x1010101) >> 24)

			data1A.Store(dest[p:])
			p += shuf1Len
			data2A.Store(dest[p:])
			p += shuf2Len
			data1B.Store(dest[p:])
			p += shuf3Len
			data2B.Store(dest[p:])
			p += shuf4Len
			col += shufTotalLen

			if col < 0 {
				continue
			}

			// we overflowed - find correct position to revert back to
			revert := col
			len2ndHalf := shuf3Len + shuf4Len
			shiftAmt := len2ndHalf - col - 1
			var eqMaskHalf uint32
			if shiftAmt < 0 {
				eqMaskHalf = (uint32(encExpandLUT[m2]) << shuf1Len) | uint32(encExpandLUT[m1])
				eqMaskHalf >>= shufTotalLen - col - 1
				pos += len2ndHalf - 16
			} else {
				eqMaskHalf = (uint32(encExpandLUT[m4]) << shuf3Len) | uint32(encExpandLUT[m3])
				eqMaskHalf >>= shiftAmt
			}
			revert += int(eqMaskHalf & 1)
			pos += bits.OnesCount32(eqMaskHalf)
			p -= revert
			pos -= revert
		} else {
			// at most one escape: shift the tail along by one byte instead
			bitIndex := bits.LeadingZeros64(mask)
			vClz := archsimd.BroadcastUint8x16(uint8(bitIndex &^ 64))
			blendA := encBlendPosA.GreaterEqual(vClz)
			blendB := encBlendPosB.GreaterEqual(vClz)

			dataAShifted := dataA.ConcatShiftBytesRight(dataA, 15)
			dataBShifted := dataB.ConcatShiftBytesRight(dataA, 15)
			outDataA := broadcastEQ.IfElse(cmpA.Equal(broadcastFF), dataA)
			outDataB := broadcastEQ.IfElse(cmpB.Equal(broadcastFF), dataB)
			outDataA = outDataA.IfElse(blendA, dataAShifted)
			outDataB = outDataB.IfElse(blendB, dataBShifted)

			outDataA.Store(dest[p:])
			outDataB.Store(dest[p+encVecSize:])
			p += encVecSize * 2
			// the byte pushed out of dataB by the shift
			dest[p] = dataB.GetElem(15)
			escaped := 0
			if mask != 0 {
				escaped = 1
			}
			p += escaped
			col += escaped + encVecSize*2

			if col < 0 {
				continue
			}

			// map the 64-bit bit index onto a byte offset from the end
			bitIndex -= ((bitIndex + 4) >> 4) << 3
			bitIndex++
			if col == bitIndex {
				// this is an escape character, so line will need to overflow
				p--
			} else if col > bitIndex {
				pos++
			}
			p -= col
			pos -= col
		}

		pos, p, col = encodeEOLHandlePre(src, pos, dest, p, lineSizeOffset)
	}

	*colOffset = col + lineSize - 1
	return pos, p
}

// encodeEOLHandlePre writes the last character of the line, the CRLF, and the
// first 31 characters of the next line
func encodeEOLHandlePre(src []byte, pos int, dest []byte, p int, lineSizeOffset int) (int, int, int) {
	oDataA := archsimd.LoadUint8x16(src[pos:])
	oDataB := archsimd.LoadUint8x16(src[pos+encVecSize:])

	// the first two lanes also escape space, tab and a leading dot
	idx := oDataA.BitsToInt8().Average(encEolIndexBias).ToBits()
	special := encEolLutLo.LookupOrZero(idx).
		Or(encEolLutHi.LookupOrZero(idx.Sub(broadcast16))).
		Or(broadcastEqM42.And(idx.GreaterEqual(broadcast32).ToInt8x16().ToBits()))
	cmpA := special.Equal(oDataA).ToInt8x16().ToBits()

	dataB := oDataB.Add(broadcast42)
	cmpEqB := oDataB.Equal(broadcastEqM42).ToInt8x16().ToBits()
	cmpB := encSpecialChars(cmpEqB, dataB)

	dataA := oDataA.Add(broadcastNeg106.IfElse(cmpA.Equal(broadcastFF), broadcast42))
	dataB = dataB.Or(cmpB.And(broadcast64))

	cmpMerge, mask := encMask(cmpA, cmpB)

	// write out first char + newline
	firstChar := uint32(dataA.GetElem(0))
	if mask&1 != 0 {
		binary.LittleEndian.PutUint32(dest[p:], (firstChar<<8)|0x0a0d003d)
		p += 4
		mask ^= 1
		cmpMerge = cmpMerge.AndNot(encClearFirstBit)
	} else {
		binary.LittleEndian.PutUint32(dest[p:], firstChar|0x0a0d00)
		p += 3
	}

	var col int
	if mask&(mask-1) != 0 {
		mask |= mask >> 8
		m1 := mask & 0xff
		m2 := (mask >> 16) & 0xff
		m3 := (mask >> 32) & 0xff
		m4 := (mask >> 48) & 0xff

		data1A := encExpand(dataA, m1)
		data2A := encExpand(dataA.ConcatShiftBytesRight(dataA, 8), m2)
		data1B := encExpand(dataB, m3)
		data2B := encExpand(dataB.ConcatShiftBytesRight(dataB, 8), m4)

		// shift out processed byte (last char of line)
		data1A = data1A.ConcatShiftBytesRight(data1A, 1)

		counts := encCounts(cmpMerge, 0x08080807)
		shuf1Len := int(counts & 0xff)
		shuf2Len := int((counts >> 8) & 0xff)
		shuf3Len := int((counts >> 16) & 0xff)
		shuf4Len := int((counts >> 24) & 0xff)
		shufTotalLen := int((counts * 0x1010101) >> 24)

		data1A.Store(dest[p:])
		p += shuf1Len
		data2A.Store(dest[p:])
		p += shuf2Len
		data1B.Store(dest[p:])
		p += shuf3Len
		data2B.Store(dest[p:])
		p += shuf4Len
		col = shufTotalLen + 1 + lineSizeOffset - 32
	} else {
		bitIndex := bits.LeadingZeros64(mask)
		vClz := archsimd.BroadcastUint8x16(uint8(bitIndex &^ 64))
		blendA := encBlendPosA.Greater(vClz)
		blendB := encBlendPosB.Greater(vClz)

		dataAShifted := broadcastEQ.IfElse(cmpA.Equal(broadcastFF), dataA)
		dataBShifted := broadcastEQ.IfElse(cmpB.Equal(broadcastFF), dataB)
		dataAShifted = dataBShifted.ConcatShiftBytesRight(dataAShifted, 1)
		dataBShifted = dataBShifted.ConcatShiftBytesRight(dataBShifted, 1)
		outDataA := dataAShifted.IfElse(blendA, dataA)
		outDataB := dataBShifted.IfElse(blendB, dataB)

		outDataA.Store(dest[p:])
		outDataB.Store(dest[p+encVecSize:])
		p += encVecSize*2 - 1
		escaped := 0
		if mask != 0 {
			escaped = 1
		}
		p += escaped
		col = lineSizeOffset + escaped
	}

	pos += encVecSize * 2
	return pos, p, col
}
