//go:build goexperiment.simd

package rapidyenc

import (
	"math/bits"
	"simd/archsimd"
	"unsafe"
)

var (
	compactLUT        [32768][16]byte
	decodeIncremental func(dest, src []byte, state *State) (nSrc int, decoded []byte, end End, err error)
)

func init() {
	const tableSize = 16
	for i := range compactLUT {
		k := i
		p := 0
		for j := range tableSize {
			if (k & 1) == 0 {
				compactLUT[i][p] = byte(j)
				p++
			}
			k >>= 1
		}
		for ; p < tableSize; p++ {
			compactLUT[i][p] = 0x80
		}
	}

	if archsimd.X86.AVX2() {
		decodeIncremental = decodeAVX2
	} else {
		decodeIncremental = decodeGeneric
	}
}

func decodeAVX2(dest, src []byte, state *State) (nSrc int, decoded []byte, end End, err error) {
	return decodeSIMD(64, dest, src, state, decodeSIMDAVX2)
}

var (
	specialLut                                                      archsimd.Int8x32
	broadcastEscapeFirst, broadcastNeg42, broadcastNeg106           archsimd.Int8x32
	broadcastDOT, broadcastEQ, broadcastCR, broadcastLF, broadcastY archsimd.Int8x32
	broadcastEQY                                                    archsimd.Int16x16
	minMask1, minMask2                                              archsimd.Int8x32
	permuteA, permuteB                                              archsimd.Int8x32
	permuteBitMask                                                  archsimd.Uint8x32
)

func init() {
	broadcastEscapeFirst = archsimd.LoadInt8x32(&[32]int8{
		-42 - 64, -42, -42, -42, -42, -42, -42, -42, -42, -42, -42, -42, -42, -42, -42, -42,
		-42, -42, -42, -42, -42, -42, -42, -42, -42, -42, -42, -42, -42, -42, -42, -42,
	})

	// search for special chars
	specialLut = archsimd.LoadInt8x32(&[32]int8{
		// lower 128‑bit lane (elements 0..15)
		'.', -1, -1, -1, -1, -1, -1, -1, -1, -1, '\n', -1, -1, '\r', '=', -1,
		// upper 128‑bit lane (elements 16..31), same pattern
		'.', -1, -1, -1, -1, -1, -1, -1, -1, -1, '\n', -1, -1, '\r', '=', -1,
	})

	minMask1 = archsimd.LoadInt8x32(&[32]int8{
		0, '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.',
		'.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.',
	})
	minMask2 = archsimd.LoadInt8x32(&[32]int8{
		'.', 0, '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.',
		'.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.', '.',
	})
	permuteA = archsimd.LoadInt8x32(&[32]int8{
		0, 0, 0, 0, 1, 1, 1, 1,
		2, 2, 2, 2, 3, 3, 3, 3,
		0, 0, 0, 0, 1, 1, 1, 1,
		2, 2, 2, 2, 3, 3, 3, 3,
	})
	permuteB = archsimd.LoadInt8x32(&[32]int8{
		4, 4, 4, 4, 5, 5, 5, 5,
		6, 6, 6, 6, 7, 7, 7, 7,
		4, 4, 4, 4, 5, 5, 5, 5,
		6, 6, 6, 6, 7, 7, 7, 7,
	})
	permuteBitMask = archsimd.BroadcastUint64x4(0x8040201008040201).AsUint8x32()
	broadcastDOT = archsimd.BroadcastInt8x32('.')
	broadcastEQ = archsimd.BroadcastInt8x32('=')
	broadcastNeg42 = archsimd.BroadcastInt8x32(-42)
	broadcastCR = archsimd.BroadcastInt8x32('\r')
	broadcastLF = archsimd.BroadcastInt8x32('\n')
	broadcastEQY = archsimd.BroadcastInt16x16(0x793d)
	broadcastY = archsimd.BroadcastInt8x32('y')
	broadcastNeg106 = archsimd.BroadcastInt8x32(-42 - 64)
}

func decodeSIMDAVX2(dest, src []byte, escFirst uint64, nextMask uint16) (int, int, uint64, uint16) {
	if len(dest) < len(src) {
		panic("slice y is shorter than slice x")
	}

	consumed := 0
	produced := 0

	dest = dest[:len(src)]

	// TODO: need this?
	isRaw := true
	searchEnd := true

	var yencOffset archsimd.Int8x32
	if escFirst > 0 {
		yencOffset = broadcastEscapeFirst
	} else {
		yencOffset = broadcastNeg42
	}
	var minMask archsimd.Int8x32
	if isRaw && nextMask > 0 {
		if nextMask == 1 {
			minMask = minMask1
		} else if nextMask == 2 {
			minMask = minMask2
		} else {
			minMask = broadcastDOT
		}
	} else {
		minMask = broadcastDOT
	}

	n := len(src)
	_ = dest[0]
	for ; consumed+68 <= n; consumed += 64 {
		s := unsafe.Pointer(uintptr(unsafe.Pointer(&src[0])) + uintptr(consumed))
		d := unsafe.Pointer(uintptr(unsafe.Pointer(&dest[0])) + uintptr(produced))
		oDataA := archsimd.LoadUint8x32((*[32]uint8)(s)).AsInt8x32()
		oDataB := archsimd.LoadUint8x32((*[32]uint8)(unsafe.Pointer(uintptr(s) + uintptr(32)))).AsInt8x32()

		cmpA := oDataA.Equal(specialLut.PermuteOrZeroGrouped(oDataA.AsUint8x32().Min(minMask.AsUint8x32()).AsInt8x32()))
		cmpB := oDataB.Equal(specialLut.PermuteOrZeroGrouped(oDataB.AsUint8x32().Min(broadcastDOT.AsUint8x32()).AsInt8x32()))
		mask := uint64(cmpB.ToBits())<<32 | uint64(cmpA.ToBits())

		if mask == 0 {
			oDataA = oDataA.Add(yencOffset)
			oDataB = oDataB.Add(broadcastNeg42)
			oDataA.AsUint8x32().Store((*[32]uint8)(d))
			oDataB.AsUint8x32().Store((*[32]uint8)(unsafe.Pointer(uintptr(d) + uintptr(32))))
			produced += 64
			escFirst = 0
			yencOffset = broadcastNeg42
		} else {
			cmpEqA := oDataA.Equal(broadcastEQ)
			cmpEqB := oDataB.Equal(broadcastEQ)
			maskEq := uint64(cmpEqB.ToBits())<<32 | uint64(cmpEqA.ToBits())

			var match2EqA archsimd.Mask8x32
			var match2EqB archsimd.Mask8x32
			var match2CrXDtA archsimd.Mask8x32
			var match2CrXDtB archsimd.Mask8x32
			var partialKillDotFound uint32

			// handle \r\n. sequences
			// RFC3977 requires the first dot on a line to be stripped, due to dot-stuffing
			if (isRaw || searchEnd) && mask != maskEq {
				tmpData2A := archsimd.LoadUint8x32((*[32]uint8)(unsafe.Pointer(uintptr(s) + uintptr(2)))).AsInt8x32()
				tmpData2B := archsimd.LoadUint8x32((*[32]uint8)(unsafe.Pointer(uintptr(s) + uintptr(34)))).AsInt8x32()

				if searchEnd {
					match2EqA = broadcastEQ.Equal(tmpData2A)
					match2EqB = broadcastEQ.Equal(tmpData2B)
				}
				if isRaw {
					// find patterns of \r_.
					match2CrXDtA = oDataA.Equal(broadcastCR).And(tmpData2A.Equal(broadcastDOT))
					match2CrXDtB = oDataB.Equal(broadcastCR).And(tmpData2B.Equal(broadcastDOT))
					partialKillDotFound = match2CrXDtA.Or(match2CrXDtB).ToBits()
				}

				var match1NlA archsimd.Mask8x32
				var match1NlB archsimd.Mask8x32

				if isRaw && partialKillDotFound > 0 {
					// merge matches for \r\n.
					match1LfA := broadcastLF.Equal(archsimd.LoadUint8x32((*[32]uint8)(unsafe.Pointer(uintptr(s) + uintptr(1)))).AsInt8x32())
					match1LfB := broadcastLF.Equal(archsimd.LoadUint8x32((*[32]uint8)(unsafe.Pointer(uintptr(s) + uintptr(33)))).AsInt8x32())
					// force re-computing these to avoid register spills elsewhere
					match1NlA = match1LfA.And(broadcastCR.Equal(oDataA))
					match1NlB = match1LfB.And(broadcastCR.Equal(oDataB))
					match2NlDotA := match2CrXDtA.And(match1NlA)
					match2NlDotB := match2CrXDtB.And(match1NlB)

					if searchEnd {
						tmpData4A := archsimd.LoadUint8x32((*[32]uint8)(unsafe.Pointer(uintptr(s) + uintptr(4)))).AsInt8x32()
						tmpData4B := archsimd.LoadUint8x32((*[32]uint8)(unsafe.Pointer(uintptr(s) + uintptr(36)))).AsInt8x32()
						// match instances of \r\n.\r\n and \r\n.=y
						match3A := archsimd.LoadUint8x32((*[32]uint8)(unsafe.Pointer(uintptr(s) + uintptr(3)))).AsInt8x32()
						match3B := archsimd.LoadUint8x32((*[32]uint8)(unsafe.Pointer(uintptr(s) + uintptr(35)))).AsInt8x32()
						match3CrA := broadcastCR.Equal(match3A)
						match3CrB := broadcastCR.Equal(match3B)
						match4LfA := tmpData4A.Equal(broadcastLF)
						match4LfB := tmpData4B.Equal(broadcastLF)
						match4EqYAw := tmpData4A.AsInt16x16().Equal(broadcastEQY) // =y
						match4EqYBw := tmpData4B.AsInt16x16().Equal(broadcastEQY) // =y

						var matchEnd uint32
						{
							match3EqYA := match2EqA.And(broadcastY.Equal(match3A))
							match3EqYB := match2EqB.And(broadcastY.Equal(match3B))
							match4EqYA := match4EqYAw.ToInt16x16().ShiftAllLeft(8).AsInt8x32().ToMask()
							match4EqYB := match4EqYBw.ToInt16x16().ShiftAllLeft(8).AsInt8x32().ToMask()
							// merge \r\n and =y matches for tmpData4
							match4EndA := match3CrA.And(match4LfA).Or(match4EqYA.Or(match3EqYA.ToInt8x32().AsInt16x16().AsUint16x16().ShiftAllRight(8).AsInt16x16().AsInt8x32().ToMask()))
							match4EndB := match3CrB.And(match4LfB).Or(match4EqYB.Or(match3EqYB.ToInt8x32().AsInt16x16().AsUint16x16().ShiftAllRight(8).AsInt16x16().AsInt8x32().ToMask()))
							// merge with \r\n.
							match4EndA = match4EndA.And(match2NlDotA)
							match4EndB = match4EndB.And(match2NlDotB)
							// match \r\n=y
							match3EndA := match3EqYA.And(match1NlA)
							match3EndB := match3EqYB.And(match1NlB)
							// combine match sequences
							matchEnd = match4EndA.Or(match3EndA).Or(match4EndB.Or(match3EndB)).ToBits()
						}

						if matchEnd > 0 {
							// terminator found
							// there's probably faster ways to do this, but reverting to scalar code should be good enough
							if isRaw {
								if *(*uint8)(s) == '.' {
									nextMask = uint16(mask) & 1
								} else if *(*uint8)(unsafe.Pointer(uintptr(s) + uintptr(1))) == '.' {
									nextMask = uint16(mask) & 2
								} else {
									nextMask = 0
								}
							} else {
								nextMask = 0
							}
							break
						}
					}
					{
						mask |= uint64(match2NlDotA.ToBits()) << 2
						mask |= uint64(match2NlDotB.ToBits()) << 34
						var zeroU8x16 archsimd.Uint8x16
						hiLane := match2NlDotB.ToInt8x32().GetHi().AsUint8x16()
						shifted := zeroU8x16.ConcatShiftBytesRight(14, hiLane)
						nlDotVec := new(archsimd.Uint8x32).SetLo(shifted)
						minMask = broadcastDOT.AsUint8x32().SubSaturated(nlDotVec).AsInt8x32()
					}
				} else if searchEnd {
					partialEndFound := false
					var match3EqYA, match3EqYB archsimd.Mask8x32
					{
						match3YA := broadcastY.Equal(archsimd.LoadUint8x32((*[32]uint8)(unsafe.Pointer(uintptr(s) + uintptr(3)))).AsInt8x32())
						match3YB := broadcastY.Equal(archsimd.LoadUint8x32((*[32]uint8)(unsafe.Pointer(uintptr(s) + uintptr(35)))).AsInt8x32())
						match3EqYA = match2EqA.And(match3YA)
						match3EqYB = match2EqB.And(match3YB)
						partialEndFound = match3EqYA.Or(match3EqYB).ToBits() > 0
					}
					if partialEndFound {
						endFound := false
						{
							match1LfA := broadcastLF.Equal(archsimd.LoadUint8x32((*[32]uint8)(unsafe.Pointer(uintptr(s) + uintptr(1)))).AsInt8x32())
							match1LfB := broadcastLF.Equal(archsimd.LoadUint8x32((*[32]uint8)(unsafe.Pointer(uintptr(s) + uintptr(33)))).AsInt8x32())
							a := match3EqYA.And(match1LfA.And(oDataA.Equal(broadcastCR)))
							b := match3EqYB.And(match1LfB.And(oDataB.Equal(broadcastCR)))
							endFound = a.Or(b).ToBits() > 0
						}
						if endFound {
							if isRaw {
								if *(*uint8)(s) == '.' {
									nextMask = uint16(mask) & 1
								} else if *(*uint8)(unsafe.Pointer(uintptr(s) + uintptr(1))) == '.' {
									nextMask = uint16(mask) & 2
								} else {
									nextMask = 0
								}
							} else {
								nextMask = 0
							}
							break
						}
					}
					if isRaw {
						minMask = broadcastDOT
					}
				} else if isRaw {
					minMask = broadcastDOT
				}
			}

			maskEqShift1 := (maskEq << 1) + escFirst
			if mask&maskEqShift1 != 0 {
				maskEq = fixEqMask(maskEq, maskEqShift1)
				mask &= ^escFirst
				escFirst = maskEq >> 63
				// next, eliminate anything following a `=` from the special char mask; this eliminates cases of `=\r` so that they aren't removed
				maskEq <<= 1
				mask &= ^maskEq

				// unescape chars following `=`
				{
					// convert maskEq into vector form (i.e. reverse pmovmskb)
					vMaskEqBytes := archsimd.BroadcastUint64x4(maskEq).AsUint8x32()
					vMaskEqA := vMaskEqBytes.PermuteOrZeroGrouped(permuteA).And(permuteBitMask).AsInt8x32().ToMask()
					vMaskEqB := vMaskEqBytes.PermuteOrZeroGrouped(permuteB).And(permuteBitMask).AsInt8x32().ToMask()
					oDataA = oDataA.Add(broadcastNeg106.Merge(yencOffset, vMaskEqA))
					oDataB = oDataB.Add(broadcastNeg106.Merge(broadcastNeg42, vMaskEqB))
				}
			} else {
				escFirst = maskEq >> 63

				{
					vecA := broadcastNeg106.Merge(
						yencOffset,
						cmpEqA.ToInt8x32().AsUint8x32().ConcatShiftBytesRightGrouped(
							15,
							archsimd.BroadcastInt8x32('=').SetHi(cmpEqA.ToInt8x32().GetLo()).AsUint8x32(),
						).Equal(archsimd.BroadcastUint8x32(0xff)),
					)
					vecB := broadcastNeg106.Merge(
						broadcastNeg42,
						broadcastEQ.Equal(archsimd.LoadUint8x32((*[32]uint8)(unsafe.Pointer(uintptr(s)+uintptr(31)))).AsInt8x32()),
					)
					oDataA = oDataA.Add(vecA)
					oDataB = oDataB.Add(vecB)
				}
			}

			if escFirst > 0 {
				yencOffset = broadcastEscapeFirst
			} else {
				yencOffset = broadcastNeg42
			}

			{
				// lookup compress masks and shuffle
				oDataA = oDataA.PermuteOrZeroGrouped(new(archsimd.Uint8x32).
					SetLo(archsimd.LoadUint8x16(&compactLUT[mask&0x7fff])).
					SetHi(archsimd.LoadUint8x16(&compactLUT[(mask>>16)&0x7fff])).
					AsInt8x32())
				// Store lower 128 bits
				oDataA.GetLo().AsUint8x16().Store((*[16]uint8)(d))
				// Store upper 128 bits
				oDataA.GetHi().AsUint8x16().Store((*[16]uint8)(unsafe.Pointer(uintptr(d) + uintptr(16-bits.OnesCount64(mask&0xffff)))))

				oDataB = oDataB.PermuteOrZeroGrouped(new(archsimd.Uint8x32).
					SetLo(archsimd.LoadUint8x16(&compactLUT[(mask>>32)&0x7fff])).
					SetHi(archsimd.LoadUint8x16(&compactLUT[(mask>>48)&0x7fff])).
					AsInt8x32())
				// Store lower 128 bits
				oDataB.GetLo().AsUint8x16().Store((*[16]uint8)(unsafe.Pointer(uintptr(d) + uintptr(32-bits.OnesCount64(mask&0xffffffff)))))
				// Store upper 128 bits
				oDataB.GetHi().AsUint8x16().Store((*[16]uint8)(unsafe.Pointer(uintptr(d) + uintptr(48-bits.OnesCount64(mask&0xffffffffffff)))))
				produced += 64 - bits.OnesCount64(mask)
			}
		}
	}
	return consumed, produced, escFirst, nextMask
}

// resolve invalid sequences of = to deal with cases like '===='
// bit hack inspired from simdjson: https://youtu.be/wlvKAT7SZIQ?t=33m38s
func fixEqMask(mask, maskShift1 uint64) uint64 {
	// isolate the start of each consecutive bit group (e.g. 01011101 -> 01000101)
	start := mask & ^maskShift1

	// this strategy works by firstly separating groups that start on even/odd bits
	// generally, it doesn't matter which one (even/odd) we pick, but clearing even groups specifically allows the escFirst bit in maskShift1 to work
	// (this is because the start of the escFirst group is at index -1, an odd bit, but we can't clear it due to being < 0, so we just retain all odd groups instead)

	even := uint64(0x5555555555555555) // every even bit (01010101...)

	// obtain groups which start on an odd bit (clear groups that start on an even bit, but this leaves an unwanted trailing bit)
	oddGroups := mask + (start & even)

	// clear even bits in odd groups, whilst conversely preserving even bits in even groups
	// the `& mask` also conveniently gets rid of unwanted trailing bits
	return (oddGroups ^ even) & mask
}
